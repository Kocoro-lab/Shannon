use app_lib::ipc_logger::IpcLogLayer;
use tracing_subscriber::layer::SubscriberExt;

#[tokio::test]
async fn buffers_logs_until_renderer_ready() {
    let layer = IpcLogLayer::new_detached();
    let subscriber = tracing_subscriber::registry().with(layer.clone());
    let _guard = tracing::subscriber::set_default(subscriber);

    tracing::info!("buffered-log");

    let logs = layer.get_recent_logs(Some(10), None).await;
    assert_eq!(logs.len(), 1);

    let drained = layer.drain_buffer();
    assert_eq!(drained.len(), 1);
    assert_eq!(layer.buffer_len(), 0);
}
