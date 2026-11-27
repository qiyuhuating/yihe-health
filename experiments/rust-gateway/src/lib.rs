//! Loopback-only experimental gateway. Go remains the sole business writer.
pub mod analytics;
pub mod consumer;
pub mod native;
pub mod outbox;
pub mod rule_authority;
pub mod rule_tx;
pub mod sos;
#[cfg(test)]
mod tests;
pub mod vitals;
use axum::{
    Router,
    body::{Body, to_bytes},
    extract::{Request, State},
    http::{HeaderMap, StatusCode},
    response::Response,
    routing::{get, post},
};
use serde::Deserialize;
use serde_json::{Value, json};
use std::{net::SocketAddr, sync::Arc, time::Duration};
use tokio::sync::Semaphore;
use yihe_core::{HeartRateSeries, Point, SeriesKey, Window};

const MAX_BODY: usize = (1 << 20) + 1;
const MAX_RESPONSE: usize = 1 << 20;

#[derive(Clone)]
struct Gateway {
    upstream: SocketAddr,
    client: reqwest::Client,
    telemetry: Arc<Semaphore>,
    sos: Arc<Semaphore>,
}

pub fn router(upstream: SocketAddr) -> Result<Router, Box<dyn std::error::Error>> {
    router_with_limits(upstream, 64, 8)
}

fn router_with_limits(
    upstream: SocketAddr,
    telemetry_limit: usize,
    sos_limit: usize,
) -> Result<Router, Box<dyn std::error::Error>> {
    if !upstream.ip().is_loopback() || upstream.port() == 0 {
        return Err("experimental upstream must be loopback".into());
    }
    let client = reqwest::Client::builder()
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none())
        .retry(reqwest::retry::never())
        .timeout(Duration::from_secs(30))
        .build()?;
    let state = Gateway {
        upstream,
        client,
        telemetry: Arc::new(Semaphore::new(telemetry_limit)),
        sos: Arc::new(Semaphore::new(sos_limit)),
    };
    Ok(Router::new()
        .route("/healthz", get(|| async { StatusCode::OK }))
        .route("/api/v1/device/report", post(report))
        .route("/api/v1/device/sos", post(sos))
        .with_state(state))
}

async fn report(State(state): State<Gateway>, request: Request) -> Response {
    relay(state, request, false).await
}

async fn sos(State(state): State<Gateway>, request: Request) -> Response {
    relay(state, request, true).await
}

fn error(status: StatusCode, code: &'static str) -> Response {
    Response::builder()
        .status(status)
        .header("content-type", "application/json")
        .body(Body::from(json!({"code":code}).to_string()))
        .expect("static response")
}

async fn relay(state: Gateway, request: Request, emergency: bool) -> Response {
    let gate = if emergency {
        &state.sos
    } else {
        &state.telemetry
    };
    let Ok(_permit) = gate.try_acquire() else {
        return error(StatusCode::SERVICE_UNAVAILABLE, "GATEWAY_BUSY");
    };
    let (parts, body) = request.into_parts();
    // Preserve raw bytes, including whitespace and key order, for HMAC.
    // At most the old Go body cap + one byte is relayed for size validation.
    let raw = match tokio::time::timeout(Duration::from_secs(10), to_bytes(body, MAX_BODY)).await {
        Ok(Ok(raw)) => raw,
        Ok(Err(_)) => return error(StatusCode::PAYLOAD_TOO_LARGE, "GATEWAY_BODY_LIMIT"),
        Err(_) => return error(StatusCode::REQUEST_TIMEOUT, "GATEWAY_BODY_TIMEOUT"),
    };
    let path = if emergency {
        "/api/v1/device/sos"
    } else {
        "/api/v1/device/report"
    };
    let mut outbound = state
        .client
        .post(format!("http://{}{path}", state.upstream));
    if let Some(query) = parts.uri.query() {
        let mut url =
            reqwest::Url::parse(&format!("http://{}{path}", state.upstream)).expect("loopback URL");
        url.set_query(Some(query));
        outbound = state.client.post(url);
    }
    // Deliberate device-only boundary. Do not trust spoofed forwarding,
    // connection-specific, cookie, or web-administrator headers.
    let mut headers = HeaderMap::new();
    for name in [
        "content-type",
        "x-device-signature",
        "x-device-timestamp",
        "x-device-nonce",
    ] {
        for value in parts.headers.get_all(name) {
            headers.append(name, value.clone());
        }
    }
    let Ok(mut upstream) = outbound.headers(headers).body(raw).send().await else {
        // Uncertain commit: never retry a POST or claim success.
        return error(StatusCode::BAD_GATEWAY, "GATEWAY_UPSTREAM_UNCERTAIN");
    };
    let mut response = Response::builder().status(upstream.status());
    for name in ["content-type", "retry-after", "cache-control"] {
        for value in upstream.headers().get_all(name) {
            response = response.header(name, value);
        }
    }
    let mut bytes = Vec::new();
    loop {
        match upstream.chunk().await {
            Ok(Some(chunk)) if bytes.len() + chunk.len() <= MAX_RESPONSE => {
                bytes.extend_from_slice(&chunk)
            }
            Ok(None) => break,
            _ => return error(StatusCode::BAD_GATEWAY, "GATEWAY_UPSTREAM_UNCERTAIN"),
        }
    }
    response
        .body(Body::from(bytes))
        .expect("upstream HTTP headers")
}

fn field(value: &Value, name: &str) -> String {
    match value.get(name) {
        Some(Value::String(s)) => s.trim().to_owned(),
        Some(Value::Number(n)) => n.as_f64().map(|v| v.to_string()).unwrap_or_default(),
        Some(Value::Bool(b)) => b.to_string(),
        None | Some(Value::Null) => String::new(),
        _ => "NaN".to_owned(),
    }
}

fn valid_request_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 64
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"_.:-".contains(&b))
}

/// Native migration candidate for the Go SOS shape check only. Never decides
/// authorization, signatures, position validation, nonce or business outcome.
pub fn sos_shape(value: &Value) -> bool {
    for (name, limit) in [("deviceNo", 32), ("secret", 128)] {
        let text = field(value, name);
        if text.is_empty() || text.chars().count() > limit {
            return false;
        }
    }
    for (name, limit) in [
        ("hr", 12),
        ("bp", 12),
        ("step", 12),
        ("sleep", 12),
        ("sleepState", 1),
        ("lng", 20),
        ("lat", 20),
        ("source", 20),
        ("beTime", 19),
        ("battery", 3),
        ("appVersion", 20),
    ] {
        if field(value, name).chars().count() > limit {
            return false;
        }
    }
    let alarm = field(value, "alarm");
    if !alarm.eq_ignore_ascii_case("fall") && alarm.chars().count() > 1 {
        return false;
    }
    let epoch = match value.get("bindingVersion") {
        Some(Value::Number(n)) => n
            .as_f64()
            .is_some_and(|n| (1.0..9_223_372_036_854_775_808.0).contains(&n) && n.fract() == 0.0),
        Some(Value::String(s)) => s.trim().parse::<i64>().is_ok_and(|n| n > 0),
        _ => false,
    };
    epoch && valid_request_id(&field(value, "requestId"))
}

#[derive(Deserialize)]
pub struct CoreInput {
    resident_id: i64,
    revision: i64,
    from: i64,
    to: i64,
    points: Vec<(i64, f64)>,
}

/// Direct Rust call, no C ABI/CGO. The caller must provide an already
/// authorized committed snapshot; this is not exposed as an HTTP endpoint.
pub fn core_statistics(input: CoreInput) -> Result<Value, &'static str> {
    let key = SeriesKey {
        resident_id: input.resident_id,
        source: "watch".into(),
        source_device: "synthetic".into(),
    };
    let window = Window {
        from: input.from,
        to: input.to,
    };
    let points: Vec<Point> = input
        .points
        .into_iter()
        .map(|(at, value)| Point { at, value })
        .collect();
    let series = HeartRateSeries::from_committed(key.clone(), input.revision, window, &points)
        .map_err(|_| "invalid committed snapshot")?;
    let result = series
        .query(&key, input.revision, window)
        .map_err(|_| "snapshot miss")?;
    Ok(match result {
        Some(s) => json!({"count":s.count,"min":s.min,"max":s.max,"mean":s.mean}),
        None => json!({"count":0}),
    })
}
