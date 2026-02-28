"""Cipher GUI — Commander's encrypted message terminal for Orac communication.

Sends encrypted messages to orac_workspace/inbox/from_commander.enc
Reads encrypted replies from orac_workspace/inbox/to_commander.enc

Launch: python cipher_gui.py
"""

import tkinter as tk
from tkinter import scrolledtext
from pathlib import Path
import sys
import os

# Add tools dir to path for cipher import
sys.path.insert(0, os.path.dirname(__file__))
from cipher import encrypt, decrypt, send_to_orac, read_from_orac, INBOX_DIR

# #region gui
class CipherGUI:
    def __init__(self):
        self.root = tk.Tk()
        self.root.title("ORAC Cipher Terminal")
        self.root.geometry("700x600")
        self.root.configure(bg="#1a1a2e")
        self.root.resizable(True, True)

        self._build_ui()

    def _build_ui(self):
        # Title
        title = tk.Label(
            self.root, text="ORAC CIPHER TERMINAL",
            font=("Consolas", 16, "bold"), fg="#00ff88", bg="#1a1a2e",
        )
        title.pack(pady=(10, 5))

        subtitle = tk.Label(
            self.root, text="Encrypted Channel: Commander \u2194 Orac",
            font=("Consolas", 10), fg="#888888", bg="#1a1a2e",
        )
        subtitle.pack(pady=(0, 10))

        # Compose frame
        compose_frame = tk.LabelFrame(
            self.root, text=" Compose Message ",
            font=("Consolas", 10), fg="#00cc66", bg="#1a1a2e",
            bd=1, relief="groove",
        )
        compose_frame.pack(fill="x", padx=15, pady=(0, 5))

        self.compose_box = scrolledtext.ScrolledText(
            compose_frame, height=4, font=("Consolas", 11),
            bg="#0d1117", fg="#e6edf3", insertbackground="#00ff88",
            wrap="word", bd=0, padx=8, pady=8,
        )
        self.compose_box.pack(fill="x", padx=5, pady=5)

        # Buttons frame
        btn_frame = tk.Frame(self.root, bg="#1a1a2e")
        btn_frame.pack(fill="x", padx=15, pady=5)

        send_btn = tk.Button(
            btn_frame, text="\u25b6  Send Encrypted",
            font=("Consolas", 11, "bold"), fg="#1a1a2e", bg="#00ff88",
            activebackground="#00cc66", activeforeground="#1a1a2e",
            bd=0, padx=20, pady=6, cursor="hand2",
            command=self._send,
        )
        send_btn.pack(side="left", padx=(0, 10))

        check_btn = tk.Button(
            btn_frame, text="\u25bc  Check Messages",
            font=("Consolas", 11, "bold"), fg="#1a1a2e", bg="#4488ff",
            activebackground="#3366dd", activeforeground="#1a1a2e",
            bd=0, padx=20, pady=6, cursor="hand2",
            command=self._check,
        )
        check_btn.pack(side="left", padx=(0, 10))

        clear_btn = tk.Button(
            btn_frame, text="\u2715  Clear",
            font=("Consolas", 10), fg="#aaaaaa", bg="#2a2a3e",
            activebackground="#3a3a4e", activeforeground="#ffffff",
            bd=0, padx=15, pady=6, cursor="hand2",
            command=self._clear,
        )
        clear_btn.pack(side="right")

        # Encrypted output frame
        enc_frame = tk.LabelFrame(
            self.root, text=" Encrypted (wire format) ",
            font=("Consolas", 10), fg="#ff8844", bg="#1a1a2e",
            bd=1, relief="groove",
        )
        enc_frame.pack(fill="x", padx=15, pady=(5, 5))

        self.enc_box = scrolledtext.ScrolledText(
            enc_frame, height=3, font=("Consolas", 10),
            bg="#0d1117", fg="#ff8844",
            wrap="char", bd=0, padx=8, pady=8, state="disabled",
        )
        self.enc_box.pack(fill="x", padx=5, pady=5)

        # Decrypted message frame
        msg_frame = tk.LabelFrame(
            self.root, text=" Orac's Message (decrypted) ",
            font=("Consolas", 10), fg="#44bbff", bg="#1a1a2e",
            bd=1, relief="groove",
        )
        msg_frame.pack(fill="both", expand=True, padx=15, pady=(5, 5))

        self.msg_box = scrolledtext.ScrolledText(
            msg_frame, height=6, font=("Consolas", 11),
            bg="#0d1117", fg="#e6edf3",
            wrap="word", bd=0, padx=8, pady=8, state="disabled",
        )
        self.msg_box.pack(fill="both", expand=True, padx=5, pady=5)

        # Status bar
        self.status_var = tk.StringVar(value="Ready.")
        status_bar = tk.Label(
            self.root, textvariable=self.status_var,
            font=("Consolas", 9), fg="#666666", bg="#12121e",
            anchor="w", padx=10, pady=4,
        )
        status_bar.pack(fill="x", side="bottom")

    def _send(self):
        message = self.compose_box.get("1.0", "end").strip()
        if not message:
            self._status("Nothing to send.")
            return

        try:
            encrypted = send_to_orac(message)
            self._set_enc(encrypted)
            self.compose_box.delete("1.0", "end")
            self._status(f"Sent encrypted ({len(encrypted)} chars) -> inbox/from_commander.enc")
        except Exception as e:
            self._status(f"Send failed: {e}")

    def _check(self):
        try:
            result = read_from_orac()
            if result is None:
                self._set_msg("")
                self._set_enc("")
                self._status("No message from Orac.")
                return

            encrypted, decrypted = result
            self._set_enc(encrypted)
            self._set_msg(decrypted)
            self._status(f"Message received ({len(decrypted)} chars)")
        except Exception as e:
            self._status(f"Read failed: {e}")

    def _clear(self):
        self.compose_box.delete("1.0", "end")
        self._set_enc("")
        self._set_msg("")
        self._status("Cleared.")

    def _set_enc(self, text: str):
        self.enc_box.config(state="normal")
        self.enc_box.delete("1.0", "end")
        self.enc_box.insert("1.0", text)
        self.enc_box.config(state="disabled")

    def _set_msg(self, text: str):
        self.msg_box.config(state="normal")
        self.msg_box.delete("1.0", "end")
        self.msg_box.insert("1.0", text)
        self.msg_box.config(state="disabled")

    def _status(self, text: str):
        self.status_var.set(text)

    def run(self):
        self.root.mainloop()
# #endregion gui


if __name__ == "__main__":
    CipherGUI().run()
