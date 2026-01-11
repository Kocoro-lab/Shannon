//! API key encryption using AES-256-GCM.
//!
//! Provides secure encryption and decryption for storing API keys in SQLite.
//! Uses AES-256-GCM AEAD cipher with random nonces for each encryption operation.

use aes_gcm::{
    aead::{Aead, KeyInit},
    Aes256Gcm, Nonce,
};
use anyhow::{Context, Result};
use base64::{engine::general_purpose, Engine as _};
use rand::{thread_rng, RngCore};
use std::path::PathBuf;

/// Encryption key size (32 bytes for AES-256).
const KEY_SIZE: usize = 32;

/// Nonce size (12 bytes recommended for AES-GCM).
const NONCE_SIZE: usize = 12;

/// Key manager for API key encryption.
///
/// Manages encryption keys and provides methods for encrypting and decrypting
/// sensitive data such as API keys.
pub struct KeyManager {
    cipher: Aes256Gcm,
}

impl std::fmt::Debug for KeyManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("KeyManager")
            .field("cipher", &"<encrypted>")
            .finish()
    }
}

impl KeyManager {
    /// Create a new key manager.
    ///
    /// Loads an existing encryption key from the specified path, or generates
    /// a new key if the file doesn't exist.
    ///
    /// # Parameters
    /// - `key_path`: Path to the encryption key file
    ///
    /// # Returns
    /// A new `KeyManager` instance
    ///
    /// # Errors
    /// Returns error if:
    /// - Key file cannot be read or written
    /// - Key file contains invalid data
    /// - Key generation fails
    pub fn new(key_path: &PathBuf) -> Result<Self> {
        let key = Self::load_or_generate_key(key_path)?;
        let cipher = Aes256Gcm::new(&key.into());
        Ok(Self { cipher })
    }

    /// Create a KeyManager using the default key path.
    ///
    /// Uses `~/.shannon/encryption.key` as the default location.
    ///
    /// # Returns
    /// A new `KeyManager` instance
    ///
    /// # Errors
    /// Returns error if key cannot be loaded or generated
    pub fn from_default_path() -> Result<Self> {
        let home = std::env::var("HOME")
            .or_else(|_| std::env::var("USERPROFILE"))
            .context("Could not determine home directory")?;
        let key_path = PathBuf::from(home).join(".shannon").join("encryption.key");
        Self::new(&key_path)
    }

    /// Encrypt a plaintext API key.
    ///
    /// Uses AES-256-GCM with a random nonce for each encryption operation.
    /// The result includes the nonce prepended to the ciphertext, both base64-encoded.
    ///
    /// # Parameters
    /// - `plaintext`: The API key to encrypt
    ///
    /// # Returns
    /// Base64-encoded string containing nonce + ciphertext
    ///
    /// # Errors
    /// Returns error if encryption fails
    pub fn encrypt(&self, plaintext: &str) -> Result<String> {
        // Generate random nonce
        let mut nonce_bytes = [0u8; NONCE_SIZE];
        thread_rng().fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from_slice(&nonce_bytes);

        // Encrypt
        let ciphertext = self
            .cipher
            .encrypt(nonce, plaintext.as_bytes())
            .map_err(|e| anyhow::anyhow!("Encryption failed: {}", e))?;

        // Combine nonce + ciphertext
        let mut result = nonce_bytes.to_vec();
        result.extend_from_slice(&ciphertext);

        // Base64 encode
        Ok(general_purpose::STANDARD.encode(result))
    }

    /// Decrypt an encrypted API key.
    ///
    /// Expects a base64-encoded string containing nonce + ciphertext.
    ///
    /// # Parameters
    /// - `encrypted`: Base64-encoded encrypted data
    ///
    /// # Returns
    /// Decrypted plaintext string
    ///
    /// # Errors
    /// Returns error if:
    /// - Input is not valid base64
    /// - Input is too short
    /// - Decryption fails (wrong key, corrupted data, etc.)
    pub fn decrypt(&self, encrypted: &str) -> Result<String> {
        // Base64 decode
        let combined = general_purpose::STANDARD
            .decode(encrypted)
            .context("Invalid base64")?;

        // Split nonce and ciphertext
        if combined.len() < NONCE_SIZE {
            anyhow::bail!("Invalid encrypted data: too short");
        }

        let (nonce_bytes, ciphertext) = combined.split_at(NONCE_SIZE);
        let nonce = Nonce::from_slice(nonce_bytes);

        // Decrypt
        let plaintext = self
            .cipher
            .decrypt(nonce, ciphertext)
            .map_err(|e| anyhow::anyhow!("Decryption failed: {}", e))?;

        String::from_utf8(plaintext).context("Invalid UTF-8 in decrypted data")
    }

    /// Load encryption key from file or generate a new one.
    ///
    /// If the key file exists, it is loaded and validated.
    /// If it doesn't exist, a new key is generated and saved with secure permissions.
    ///
    /// # Parameters
    /// - `path`: Path to the key file
    ///
    /// # Returns
    /// 32-byte encryption key
    ///
    /// # Errors
    /// Returns error if file operations fail or key is invalid
    fn load_or_generate_key(path: &PathBuf) -> Result<[u8; KEY_SIZE]> {
        if path.exists() {
            // Load existing key
            let encoded =
                std::fs::read_to_string(path).context("Failed to read encryption key file")?;
            let bytes = general_purpose::STANDARD
                .decode(encoded.trim())
                .context("Invalid base64 in key file")?;

            if bytes.len() != KEY_SIZE {
                anyhow::bail!(
                    "Invalid key size: expected {}, got {}",
                    KEY_SIZE,
                    bytes.len()
                );
            }

            let mut key = [0u8; KEY_SIZE];
            key.copy_from_slice(&bytes);
            Ok(key)
        } else {
            // Generate new key
            let mut key = [0u8; KEY_SIZE];
            thread_rng().fill_bytes(&mut key);

            // Save to file
            if let Some(parent) = path.parent() {
                std::fs::create_dir_all(parent).context("Failed to create key directory")?;
            }

            let encoded = general_purpose::STANDARD.encode(key);
            std::fs::write(path, encoded).context("Failed to write encryption key file")?;

            // Set restrictive permissions (Unix only)
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                let mut perms = std::fs::metadata(path)
                    .context("Failed to get key file metadata")?
                    .permissions();
                perms.set_mode(0o600); // Read/write for owner only
                std::fs::set_permissions(path, perms)
                    .context("Failed to set key file permissions")?;
            }

            Ok(key)
        }
    }

    /// Mask an API key for display.
    ///
    /// Shows only the first and last few characters, replacing the middle with "...".
    ///
    /// # Parameters
    /// - `key`: The API key to mask
    ///
    /// # Returns
    /// Masked version of the key (e.g., "sk-...xyz")
    pub fn mask_key(&self, key: &str) -> String {
        if key.len() <= 6 {
            return "***".to_string();
        }
        format!("{}...{}", &key[..3], &key[key.len() - 3..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn test_encrypt_decrypt_roundtrip() {
        let dir = tempdir().expect("Failed to create temp dir");
        let key_path = dir.path().join("test.key");

        let km = KeyManager::new(&key_path).expect("Failed to create KeyManager");
        let plaintext = "sk-proj-test123456789";

        let encrypted = km.encrypt(plaintext).expect("Encryption failed");
        let decrypted = km.decrypt(&encrypted).expect("Decryption failed");

        assert_eq!(plaintext, decrypted);
        assert_ne!(plaintext, encrypted);
    }

    #[test]
    fn test_multiple_encryptions_different() {
        let dir = tempdir().expect("Failed to create temp dir");
        let key_path = dir.path().join("test.key");

        let km = KeyManager::new(&key_path).expect("Failed to create KeyManager");
        let plaintext = "sk-proj-test123";

        let encrypted1 = km.encrypt(plaintext).expect("Encryption 1 failed");
        let encrypted2 = km.encrypt(plaintext).expect("Encryption 2 failed");

        // Different nonces should produce different ciphertexts
        assert_ne!(encrypted1, encrypted2);

        // Both should decrypt correctly
        assert_eq!(
            plaintext,
            km.decrypt(&encrypted1).expect("Decryption 1 failed")
        );
        assert_eq!(
            plaintext,
            km.decrypt(&encrypted2).expect("Decryption 2 failed")
        );
    }

    #[test]
    fn test_key_persistence() {
        let dir = tempdir().expect("Failed to create temp dir");
        let key_path = dir.path().join("test.key");

        let plaintext = "sk-proj-persistent";

        // Create first KeyManager and encrypt
        let encrypted = {
            let km = KeyManager::new(&key_path).expect("Failed to create KeyManager 1");
            km.encrypt(plaintext).expect("Encryption failed")
        };

        // Create second KeyManager (should load same key)
        let km2 = KeyManager::new(&key_path).expect("Failed to create KeyManager 2");
        let decrypted = km2.decrypt(&encrypted).expect("Decryption failed");

        assert_eq!(plaintext, decrypted);
    }

    #[test]
    fn test_invalid_encrypted_data() {
        let dir = tempdir().expect("Failed to create temp dir");
        let key_path = dir.path().join("test.key");

        let km = KeyManager::new(&key_path).expect("Failed to create KeyManager");

        // Invalid base64
        assert!(km.decrypt("not-base64!@#$").is_err());

        // Too short
        assert!(km
            .decrypt(&general_purpose::STANDARD.encode([1, 2, 3]))
            .is_err());

        // Valid base64 but wrong ciphertext
        assert!(km
            .decrypt(&general_purpose::STANDARD.encode([0u8; 32]))
            .is_err());
    }

    #[test]
    fn test_mask_key() {
        let dir = tempdir().expect("Failed to create temp dir");
        let key_path = dir.path().join("test.key");
        let km = KeyManager::new(&key_path).expect("Failed to create KeyManager");

        assert_eq!(km.mask_key("sk-proj-abc123xyz"), "sk-...xyz");
        assert_eq!(km.mask_key("short"), "***");
        assert_eq!(km.mask_key("sk-abc"), "sk-...abc");
    }

    #[test]
    fn test_unicode_encryption() {
        let dir = tempdir().expect("Failed to create temp dir");
        let key_path = dir.path().join("test.key");

        let km = KeyManager::new(&key_path).expect("Failed to create KeyManager");
        let plaintext = "sk-测试-🔑-encryption";

        let encrypted = km.encrypt(plaintext).expect("Encryption failed");
        let decrypted = km.decrypt(&encrypted).expect("Decryption failed");

        assert_eq!(plaintext, decrypted);
    }
}
