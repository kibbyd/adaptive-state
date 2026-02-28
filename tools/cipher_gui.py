"""Cipher GUI — Commander's encrypted message terminal for Orac communication.

Sends encrypted messages to orac_workspace/inbox/from_commander.enc
Reads encrypted replies from orac_workspace/inbox/to_commander.enc

Auto-polls for responses every 2s. Shows delivery confirmation when
controller picks up the message (from_commander.enc disappears).

Launch: python cipher_gui.py
"""

import tkinter as tk
from tkinter import scrolledtext
from pathlib import Path
from datetime import datetime
import sys
import os

# Add tools dir to path for cipher import
sys.path.insert(0, os.path.dirname(__file__))
from cipher import send_to_orac, read_from_orac, INBOX_DIR

# #region gui
class CipherGUI:
    def __init__(self):
        self.root = tk.Tk()
        self.root.title("ORAC Cipher Terminal")
        self.root.geometry("700x650")
        self.root.configure(bg="#1a1a2e")
        self.root.resizable(True, True)

        self._waiting_for_pickup = False  # tracking delivery confirmation
        self._waiting_for_reply = False   # tracking Orac's response
        self._last_reply_enc = None       # dedup repeated poll reads

        self._build_ui()
        self._start_polling()

    def _build_ui(self):
        # Title
        title = tk.Label(
            self.root, text="ORAC CIPHER TERMINAL",
            font=("Consolas", 16, "bold"), fg="#00ff88", bg="#1a1a2e",
        )
        title.pack(pady=(10, 2))

        subtitle = tk.Label(
            self.root, text="Encrypted Channel: Commander \u2194 Orac",
            font=("Consolas", 10), fg="#888888", bg="#1a1a2e",
        )
        subtitle.pack(pady=(0, 8))

        # Chat log frame (scrolling conversation)
        chat_frame = tk.LabelFrame(
            self.root, text=" Conversation ",
            font=("Consolas", 10), fg="#44bbff", bg="#1a1a2e",
            bd=1, relief="groove",
        )
        chat_frame.pack(fill="both", expand=True, padx=15, pady=(0, 5))

        self.chat_log = scrolledtext.ScrolledText(
            chat_frame, height=14, font=("Consolas", 11),
            bg="#0d1117", fg="#e6edf3",
            wrap="word", bd=0, padx=8, pady=8, state="disabled",
        )
        self.chat_log.pack(fill="both", expand=True, padx=5, pady=5)

        # Configure chat tags for coloring
        self.chat_log.tag_configure("commander", foreground="#00ff88")
        self.chat_log.tag_configure("orac", foreground="#44bbff")
        self.chat_log.tag_configure("system", foreground="#666666")
        self.chat_log.tag_configure("timestamp", foreground="#555555")

        # Compose frame
        compose_frame = tk.LabelFrame(
            self.root, text=" Compose Message ",
            font=("Consolas", 10), fg="#00cc66", bg="#1a1a2e",
            bd=1, relief="groove",
        )
        compose_frame.pack(fill="x", padx=15, pady=(0, 5))

        self.compose_box = scrolledtext.ScrolledText(
            compose_frame, height=3, font=("Consolas", 11),
            bg="#0d1117", fg="#e6edf3", insertbackground="#00ff88",
            wrap="word", bd=0, padx=8, pady=8,
        )
        self.compose_box.pack(fill="x", padx=5, pady=5)
        # Enter to send, Shift+Enter for newline
        self.compose_box.bind("<Return>", self._on_enter)

        # Buttons frame
        btn_frame = tk.Frame(self.root, bg="#1a1a2e")
        btn_frame.pack(fill="x", padx=15, pady=5)

        send_btn = tk.Button(
            btn_frame, text="\u25b6  Send",
            font=("Consolas", 11, "bold"), fg="#1a1a2e", bg="#00ff88",
            activebackground="#00cc66", activeforeground="#1a1a2e",
            bd=0, padx=20, pady=6, cursor="hand2",
            command=self._send,
        )
        send_btn.pack(side="left", padx=(0, 10))

        clear_btn = tk.Button(
            btn_frame, text="\u2715  Clear Log",
            font=("Consolas", 10), fg="#aaaaaa", bg="#2a2a3e",
            activebackground="#3a3a4e", activeforeground="#ffffff",
            bd=0, padx=15, pady=6, cursor="hand2",
            command=self._clear_log,
        )
        clear_btn.pack(side="right")

        # Status bar
        self.status_var = tk.StringVar(value="Ready. Auto-polling active.")
        status_bar = tk.Label(
            self.root, textvariable=self.status_var,
            font=("Consolas", 9), fg="#666666", bg="#12121e",
            anchor="w", padx=10, pady=4,
        )
        status_bar.pack(fill="x", side="bottom")

    def _on_enter(self, event):
        """Enter sends, Shift+Enter inserts newline."""
        if event.state & 0x1:  # Shift held
            return  # let default handler insert newline
        self._send()
        return "break"  # suppress default newline insertion

    def _send(self):
        message = self.compose_box.get("1.0", "end").strip()
        if not message:
            self._status("Nothing to send.")
            return

        try:
            # Clear old response before sending to prevent echo
            old_reply = INBOX_DIR / "to_commander.enc"
            if old_reply.exists():
                old_reply.unlink()

            send_to_orac(message)
            self.compose_box.delete("1.0", "end")
            self._append_chat("COMMANDER", message, "commander")
            self._status("Encrypted and sent. Waiting for delivery...")
            self._waiting_for_pickup = True
            self._waiting_for_reply = True
        except Exception as e:
            self._status(f"Send failed: {e}")

    def _start_polling(self):
        """Start the auto-poll loop."""
        self._poll()

    def _poll(self):
        """Check for delivery confirmation and incoming replies."""
        try:
            # Check delivery: from_commander.enc disappearing = controller picked it up
            if self._waiting_for_pickup:
                outgoing_file = INBOX_DIR / "from_commander.enc"
                if not outgoing_file.exists():
                    self._waiting_for_pickup = False
                    self._append_chat("SYSTEM", "Message delivered to Orac.", "system")
                    self._status("Delivered. Waiting for Orac's response...")

            # Check for Orac's reply
            if self._waiting_for_reply or not self._waiting_for_pickup:
                result = read_from_orac()
                if result is not None:
                    encrypted, decrypted = result
                    # Dedup: only show if it's a new response
                    if encrypted != self._last_reply_enc:
                        self._last_reply_enc = encrypted
                        self._append_chat("ORAC", decrypted, "orac")
                        self._waiting_for_reply = False
                        self._status("Response received.")
        except Exception as e:
            self._status(f"Poll error: {e}")

        # Schedule next poll
        self.root.after(2000, self._poll)

    def _append_chat(self, sender, message, tag):
        """Append a message to the chat log."""
        self.chat_log.config(state="normal")
        timestamp = datetime.now().strftime("%H:%M:%S")
        self.chat_log.insert("end", f"[{timestamp}] ", "timestamp")
        self.chat_log.insert("end", f"{sender}: ", tag)
        self.chat_log.insert("end", f"{message}\n\n")
        self.chat_log.see("end")
        self.chat_log.config(state="disabled")

    def _clear_log(self):
        self.chat_log.config(state="normal")
        self.chat_log.delete("1.0", "end")
        self.chat_log.config(state="disabled")
        self._status("Log cleared.")

    def _status(self, text: str):
        self.status_var.set(text)

    def run(self):
        self.root.mainloop()
# #endregion gui


if __name__ == "__main__":
    CipherGUI().run()
