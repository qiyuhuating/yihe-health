use super::*;
use axum::{body::Bytes, extract::State, http::HeaderMap};
use std::sync::atomic::{AtomicUsize, Ordering};
use tokio::{net::TcpListener, sync::Notify, task::JoinHandle, time::timeout};

async fn serve(app: Router) -> (SocketAddr, JoinHandle<()>) {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let task = tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });
    (addr, task)
}

#[tokio::test]
async fn saturation_preserves_sos_and_signed_body() {
    #[derive(Default)]
    struct TestState {
        arrived: Notify,
        release: Notify,
        count: AtomicUsize,
    }
    let state = Arc::new(TestState::default());
    let upstream = Router::new()
        .route(
            "/api/v1/device/report",
            post(|State(s): State<Arc<TestState>>| async move {
                s.count.fetch_add(1, Ordering::SeqCst);
                s.arrived.notify_one();
                s.release.notified().await;
                StatusCode::OK
            }),
        )
        .route(
            "/api/v1/device/sos",
            post(|headers: HeaderMap, body: Bytes| async move {
                assert_eq!(headers["x-device-signature"], "fixture-signature");
                assert!(!headers.contains_key("x-forwarded-for"));
                assert!(!headers.contains_key("cookie"));
                assert_eq!(&body[..], b"{ \"x\" : 1 }");
                (
                    StatusCode::CONFLICT,
                    [("content-type", "application/json")],
                    "{\"code\":\"FIXTURE\"}",
                )
            }),
        )
        .with_state(state.clone());
    let (upstream_addr, upstream_task) = serve(upstream).await;
    let (addr, gateway_task) = serve(router_with_limits(upstream_addr, 1, 1).unwrap()).await;
    let client = reqwest::Client::builder()
        .no_proxy()
        .timeout(Duration::from_secs(5))
        .build()
        .unwrap();
    let report_url = format!("http://{addr}/api/v1/device/report");
    let first_client = client.clone();
    let first_url = report_url.clone();
    let first = tokio::spawn(async move {
        first_client
            .post(first_url)
            .body("{}")
            .send()
            .await
            .unwrap()
    });
    timeout(Duration::from_secs(2), state.arrived.notified())
        .await
        .unwrap();
    let saturated = client.post(&report_url).body("{}").send().await.unwrap();
    assert_eq!(saturated.status(), StatusCode::SERVICE_UNAVAILABLE);
    let sos = client
        .post(format!("http://{addr}/api/v1/device/sos"))
        .header("x-device-signature", "fixture-signature")
        .header("x-forwarded-for", "203.0.113.1")
        .header("cookie", "not-a-device-cookie")
        .body("{ \"x\" : 1 }")
        .send()
        .await
        .unwrap();
    assert_eq!(sos.status(), StatusCode::CONFLICT);
    assert_eq!(sos.text().await.unwrap(), "{\"code\":\"FIXTURE\"}");
    state.release.notify_one();
    assert_eq!(first.await.unwrap().status(), StatusCode::OK);
    assert_eq!(state.count.load(Ordering::SeqCst), 1);
    let oversized = client
        .post(&report_url)
        .body(vec![b'x'; MAX_BODY + 1])
        .send()
        .await
        .unwrap();
    assert_eq!(oversized.status(), StatusCode::PAYLOAD_TOO_LARGE);
    assert_eq!(state.count.load(Ordering::SeqCst), 1);
    gateway_task.abort();
    upstream_task.abort();
}

#[tokio::test]
async fn uncertain_upstream_does_not_retry() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream = listener.local_addr().unwrap();
    let count = Arc::new(AtomicUsize::new(0));
    let observed = count.clone();
    let task = tokio::spawn(async move {
        loop {
            let (stream, _) = listener.accept().await.unwrap();
            observed.fetch_add(1, Ordering::SeqCst);
            drop(stream);
        }
    });
    let (addr, gateway) = serve(router(upstream).unwrap()).await;
    let client = reqwest::Client::builder()
        .no_proxy()
        .timeout(Duration::from_secs(5))
        .build()
        .unwrap();
    let response = client
        .post(format!("http://{addr}/api/v1/device/sos"))
        .body("{}")
        .send()
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::BAD_GATEWAY);
    assert!(
        response
            .text()
            .await
            .unwrap()
            .contains("GATEWAY_UPSTREAM_UNCERTAIN")
    );
    assert_eq!(count.load(Ordering::SeqCst), 1);
    gateway.abort();
    task.abort();
}
