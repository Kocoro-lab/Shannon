use app_lib::embedded_port::select_available_port;
use tokio::net::TcpListener;

#[tokio::test]
async fn embedded_port_fallback_selects_next_available() {
    let range = 1906..=1915;
    let mut bound_listener = None;

    for port in range.clone() {
        if let Ok(listener) = TcpListener::bind(("127.0.0.1", port)).await {
            bound_listener = Some((port, listener));
            break;
        }
    }

    let Some((bound_port, _listener)) = bound_listener else {
        eprintln!("No available ports in range for test; skipping.");
        return;
    };

    let selected = select_available_port(range, Some(bound_port)).await;
    if selected.is_none() {
        eprintln!("No fallback ports available after binding {}; skipping.", bound_port);
        return;
    }

    let selected_port = selected.unwrap();
    assert_ne!(selected_port, bound_port);
    assert!((1906..=1915).contains(&selected_port));
}
