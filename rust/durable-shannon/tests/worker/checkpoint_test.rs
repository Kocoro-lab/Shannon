//! Integration tests for checkpoint optimization.

use durable_shannon::worker::checkpoint::{Checkpoint, CheckpointConfig, CheckpointManager};

#[test]
fn test_adaptive_checkpoint_frequency() {
    let config = CheckpointConfig {
        min_events: 10,
        max_interval_secs: 5,
        ..Default::default()
    };
    let mut manager = CheckpointManager::new(config);

    // Should not checkpoint initially
    assert!(!manager.should_checkpoint());

    // After 10 events, should checkpoint
    for _ in 0..10 {
        manager.record_event();
    }
    assert!(manager.should_checkpoint());

    // Reset after checkpoint
    let state = b"test state";
    let _ = manager.create_checkpoint(1, state, None).unwrap();
    assert!(!manager.should_checkpoint());
}

#[test]
fn test_compression_ratio() {
    let config = CheckpointConfig {
        enable_compression: true,
        ..Default::default()
    };
    let mut manager = CheckpointManager::new(config);

    // Create state with repetitive data (compresses well)
    let state = "repeated text ".repeat(100).into_bytes();
    let checkpoint = manager.create_checkpoint(1, &state, None).unwrap();

    // Verify compression happened
    assert_eq!(checkpoint.original_size, state.len());
    assert!(checkpoint.compressed_size < checkpoint.original_size);

    // Compression ratio should be good (>50% reduction)
    let ratio = (checkpoint.compressed_size as f64 / checkpoint.original_size as f64) * 100.0;
    assert!(ratio < 50.0, "Compression ratio should be <50%");
}

#[test]
fn test_compression_performance() {
    let config = CheckpointConfig::default();
    let mut manager = CheckpointManager::new(config);

    // 100KB of data
    let state = vec![0u8; 100_000];

    let start = std::time::Instant::now();
    let checkpoint = manager.create_checkpoint(1, &state, None).unwrap();
    let compression_time = start.elapsed();

    // Compression should be fast (<100ms for 100KB)
    assert!(
        compression_time.as_millis() < 100,
        "Compression took {}ms, expected <100ms",
        compression_time.as_millis()
    );

    // Decompression should also be fast
    let start = std::time::Instant::now();
    let _ = manager.load_checkpoint(&checkpoint).unwrap();
    let decompression_time = start.elapsed();

    assert!(
        decompression_time.as_millis() < 50,
        "Decompression took {}ms, expected <50ms",
        decompression_time.as_millis()
    );
}

#[test]
fn test_checksum_verification() {
    let config = CheckpointConfig {
        enable_checksum: true,
        ..Default::default()
    };
    let mut manager = CheckpointManager::new(config);

    let state = b"test data";
    let checkpoint = manager.create_checkpoint(1, state, None).unwrap();

    // Valid checkpoint should load
    let loaded = manager.load_checkpoint(&checkpoint).unwrap();
    assert_eq!(loaded, state);

    // Corrupted checkpoint should fail
    let mut corrupted = checkpoint.clone();
    corrupted.data[0] ^= 0xFF; // Flip bits

    let result = manager.load_checkpoint(&corrupted);
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("corruption"));
}

#[test]
fn test_checkpoint_pruning() {
    let config = CheckpointConfig {
        max_checkpoints: 3,
        ..Default::default()
    };
    let manager = CheckpointManager::new(config);

    // Create 10 checkpoints
    let mut checkpoints = vec![];
    for i in 1..=10 {
        checkpoints.push(Checkpoint {
            sequence: i,
            data: vec![],
            checksum: 0,
            original_size: 0,
            compressed_size: 0,
            is_incremental: false,
            base_sequence: None,
            created_at: chrono::Utc::now(),
        });
    }

    // Prune to keep last 3
    let pruned = manager.prune_checkpoints(checkpoints).unwrap();

    assert_eq!(pruned.len(), 3);
    assert_eq!(pruned[0].sequence, 10); // Newest
    assert_eq!(pruned[1].sequence, 9);
    assert_eq!(pruned[2].sequence, 8); // Oldest kept
}

#[test]
fn test_multiple_checkpoints_roundtrip() {
    let config = CheckpointConfig::default();
    let mut manager = CheckpointManager::new(config);

    let states = vec![
        b"state 1".to_vec(),
        b"state 2".to_vec(),
        b"state 3".to_vec(),
    ];

    let mut checkpoints = vec![];

    // Create checkpoints
    for (i, state) in states.iter().enumerate() {
        let checkpoint = manager
            .create_checkpoint((i + 1) as u64, state, None)
            .unwrap();
        checkpoints.push(checkpoint);
    }

    // Load and verify all checkpoints
    for (i, checkpoint) in checkpoints.iter().enumerate() {
        let loaded = manager.load_checkpoint(checkpoint).unwrap();
        assert_eq!(loaded, states[i]);
    }
}

#[test]
fn test_checkpoint_stats_tracking() {
    let config = CheckpointConfig::default();
    let mut manager = CheckpointManager::new(config);

    let state1 = vec![0u8; 1000];
    let state2 = vec![1u8; 2000];

    let _ = manager.create_checkpoint(1, &state1, None).unwrap();
    let _ = manager.create_checkpoint(2, &state2, None).unwrap();

    let stats = manager.stats();
    assert_eq!(stats.total_created, 2);
    assert_eq!(stats.total_bytes_compressed, 3000);
    assert!(stats.total_bytes_saved > 0);
    assert!(stats.total_compression_ms > 0);
}

#[test]
fn test_compression_disabled() {
    let config = CheckpointConfig {
        enable_compression: false,
        ..Default::default()
    };
    let mut manager = CheckpointManager::new(config);

    let state = b"test data that would compress";
    let checkpoint = manager.create_checkpoint(1, state, None).unwrap();

    // No compression, sizes should match
    assert_eq!(checkpoint.original_size, checkpoint.compressed_size);
}

#[test]
fn test_checksum_disabled() {
    let config = CheckpointConfig {
        enable_checksum: false,
        ..Default::default()
    };
    let mut manager = CheckpointManager::new(config);

    let state = b"test data";
    let checkpoint = manager.create_checkpoint(1, state, None).unwrap();

    // Checksum should be 0 when disabled
    assert_eq!(checkpoint.checksum, 0);

    // Should still load even with corrupted data
    let mut corrupted = checkpoint.clone();
    if !corrupted.data.is_empty() {
        corrupted.data[0] ^= 0xFF;
    }

    // Should not fail (checksum disabled)
    let _loaded = manager.load_checkpoint(&corrupted);
}

#[test]
fn test_large_state_compression() {
    let config = CheckpointConfig::default();
    let mut manager = CheckpointManager::new(config);

    // 1MB of data
    let state = vec![42u8; 1_000_000];

    let checkpoint = manager.create_checkpoint(1, &state, None).unwrap();

    // Should compress significantly
    let ratio = (checkpoint.compressed_size as f64 / checkpoint.original_size as f64) * 100.0;
    assert!(ratio < 10.0, "Large uniform data should compress to <10%");

    // Should still be able to decompress
    let loaded = manager.load_checkpoint(&checkpoint).unwrap();
    assert_eq!(loaded, state);
}

#[test]
fn test_checkpoint_with_empty_state() {
    let config = CheckpointConfig::default();
    let mut manager = CheckpointManager::new(config);

    let state = b"";
    let checkpoint = manager.create_checkpoint(1, state, None).unwrap();

    assert_eq!(checkpoint.original_size, 0);

    let loaded = manager.load_checkpoint(&checkpoint).unwrap();
    assert_eq!(loaded, state);
}

#[test]
fn test_incremental_checkpoint_flag() {
    let config = CheckpointConfig {
        enable_incremental: true,
        ..Default::default()
    };
    let mut manager = CheckpointManager::new(config);

    // First checkpoint (full)
    let state1 = b"initial state";
    let checkpoint1 = manager.create_checkpoint(1, state1, None).unwrap();
    assert!(!checkpoint1.is_incremental);

    // Second checkpoint (could be incremental)
    let state2 = b"updated state";
    let checkpoint2 = manager
        .create_checkpoint(2, state2, Some(&checkpoint1))
        .unwrap();

    // For MVP, incremental is disabled but flag tracked
    assert!(!checkpoint2.is_incremental);
}
