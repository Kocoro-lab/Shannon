use std::ops::RangeInclusive;
use tracing::debug;

/// Select the first available port in the range, preferring the provided port when available.
pub async fn select_available_port(
    range: RangeInclusive<u16>,
    preferred: Option<u16>,
) -> Option<u16> {
    let mut ports: Vec<u16> = range.clone().collect();

    if let Some(preferred_port) = preferred {
        if ports.contains(&preferred_port) {
            ports.retain(|port| *port != preferred_port);
            ports.insert(0, preferred_port);
        }
    }

    for port in ports {
        debug!(port = port, "Checking port availability");

        if tokio::net::TcpListener::bind(("127.0.0.1", port)).await.is_ok() {
            return Some(port);
        }
    }

    None
}
