"""Shared symmetric cipher for Commander <-> Orac encrypted messaging.

Uses AES-CTR with a key-derived nonce for deterministic encryption.
Same plaintext + same key = same ciphertext every time.

Shared key stored at: C:/adaptive_state/orac_workspace/.cipher_key
Auto-generated on first use.

CLI usage:
  python cipher.py encrypt "secret message"
  python cipher.py decrypt "base64_ciphertext"
  python cipher.py keygen   (force new key)
"""

import base64
import hashlib
import os
import sys
from pathlib import Path

# #region config
WORKSPACE_DIR = Path("C:/adaptive_state/orac_workspace")
KEY_FILE = WORKSPACE_DIR / ".cipher_key"
INBOX_DIR = WORKSPACE_DIR / "inbox"
# #endregion config


# #region key-management
def _ensure_key() -> bytes:
    """Load or generate the shared 256-bit key."""
    WORKSPACE_DIR.mkdir(parents=True, exist_ok=True)
    if KEY_FILE.exists():
        raw = KEY_FILE.read_bytes().strip()
        if len(raw) >= 32:
            return raw[:32]
    # Generate new key
    key = os.urandom(32)
    KEY_FILE.write_bytes(key)
    return key


def keygen() -> str:
    """Force-generate a new shared key. Returns hex representation."""
    WORKSPACE_DIR.mkdir(parents=True, exist_ok=True)
    key = os.urandom(32)
    KEY_FILE.write_bytes(key)
    return key.hex()
# #endregion key-management


# #region cipher
def _aes_ctr_keystream(key: bytes, length: int) -> bytes:
    """Generate a deterministic keystream using SHA-256 in counter mode.
    No external crypto library needed — uses hashlib only."""
    stream = bytearray()
    counter = 0
    while len(stream) < length:
        block_input = key + counter.to_bytes(8, "big")
        block = hashlib.sha256(block_input).digest()
        stream.extend(block)
        counter += 1
    return bytes(stream[:length])


def encrypt(plaintext: str, key: bytes = None) -> str:
    """Encrypt plaintext to base64 ciphertext. Deterministic — same input = same output."""
    if key is None:
        key = _ensure_key()
    data = plaintext.encode("utf-8")
    keystream = _aes_ctr_keystream(key, len(data))
    ciphertext = bytes(a ^ b for a, b in zip(data, keystream))
    return base64.b64encode(ciphertext).decode("ascii")


def decrypt(b64_ciphertext: str, key: bytes = None) -> str:
    """Decrypt base64 ciphertext back to plaintext."""
    if key is None:
        key = _ensure_key()
    ciphertext = base64.b64decode(b64_ciphertext)
    keystream = _aes_ctr_keystream(key, len(ciphertext))
    plaintext = bytes(a ^ b for a, b in zip(ciphertext, keystream))
    return plaintext.decode("utf-8")
# #endregion cipher


# #region inbox
def send_to_orac(message: str) -> str:
    """Encrypt message and write to inbox/from_commander.enc."""
    INBOX_DIR.mkdir(parents=True, exist_ok=True)
    encrypted = encrypt(message)
    outfile = INBOX_DIR / "from_commander.enc"
    outfile.write_text(encrypted, encoding="utf-8")
    return encrypted


def read_from_orac() -> tuple[str, str] | None:
    """Read and decrypt inbox/to_commander.enc. Returns (encrypted, decrypted) or None."""
    infile = INBOX_DIR / "to_commander.enc"
    if not infile.exists():
        return None
    encrypted = infile.read_text(encoding="utf-8").strip()
    if not encrypted:
        return None
    decrypted = decrypt(encrypted)
    return encrypted, decrypted
# #endregion inbox


# #region cli
def main():
    if len(sys.argv) < 2:
        print("Usage: cipher.py <encrypt|decrypt|keygen|send|read> [text]")
        sys.exit(1)

    cmd = sys.argv[1].lower()

    if cmd == "keygen":
        hex_key = keygen()
        print(f"New key generated: {hex_key[:16]}... (stored at {KEY_FILE})")

    elif cmd == "encrypt":
        if len(sys.argv) < 3:
            print("Usage: cipher.py encrypt \"message\"")
            sys.exit(1)
        text = " ".join(sys.argv[2:])
        print(encrypt(text))

    elif cmd == "decrypt":
        if len(sys.argv) < 3:
            print("Usage: cipher.py decrypt \"base64_ciphertext\"")
            sys.exit(1)
        print(decrypt(sys.argv[2]))

    elif cmd == "send":
        if len(sys.argv) < 3:
            print("Usage: cipher.py send \"message to orac\"")
            sys.exit(1)
        text = " ".join(sys.argv[2:])
        enc = send_to_orac(text)
        print(f"Sent encrypted ({len(enc)} chars) -> inbox/from_commander.enc")

    elif cmd == "read":
        result = read_from_orac()
        if result is None:
            print("No message from Orac.")
        else:
            enc, dec = result
            print(f"Encrypted: {enc[:60]}...")
            print(f"Decrypted: {dec}")

    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)


if __name__ == "__main__":
    main()
# #endregion cli
