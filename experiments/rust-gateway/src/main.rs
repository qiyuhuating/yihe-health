use serde_json::{Value, json};
use std::{
    io::{self, BufRead, Write},
    net::SocketAddr,
};
use yihe_next_gateway::{core_statistics, router, sos_shape};

fn dispatch_lane(mode: &str) -> yihe_next_gateway::consumer::Lane {
    use yihe_next_gateway::consumer::Lane;
    if mode.starts_with("--dispatch-sos-") {
        Lane::Sos
    } else if mode.starts_with("--dispatch-vital-") {
        Lane::Vital
    } else {
        Lane::Step
    }
}
#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    match args.as_slice() {
        [_, mode] if mode == "--rule-authority-test" => {
            yihe_next_gateway::rule_authority::run().await
        }
        [_, mode] if mode == "--rule-commit-test" => yihe_next_gateway::rule_tx::run().await,
        [_, mode] if mode == "--oracle" => {
            // Local differential test transport, not a public HTTP API.
            let stdin = io::stdin().lock();
            let mut stdout = io::BufWriter::new(io::stdout().lock());
            for line in stdin.lines() {
                let input: Value = serde_json::from_str(&line?)?;
                let result = match input.get("kind").and_then(Value::as_str) {
                    Some("sos") => json!(sos_shape(&input["body"])),
                    Some("transitions") => {
                        yihe_next_gateway::analytics::transitions(input["body"].clone())?
                    }
                    Some("rolling") => {
                        yihe_next_gateway::analytics::rolling(input["body"].clone())?
                    }
                    Some("rules") => {
                        yihe_next_gateway::analytics::decisions(input["body"].clone())?
                    }
                    Some("core") => core_statistics(serde_json::from_value(input["body"].clone())?)
                        .unwrap_or_else(|e| json!({"error":e})),
                    _ => return Err("unknown oracle case".into()),
                };
                writeln!(stdout, "{result}")?;
                stdout.flush()?;
            }
            Ok(())
        }
        [_, mode, address]
            if matches!(
                mode.as_str(),
                "--consume-one-test"
                    | "--consume-worker-test"
                    | "--consume-crash-test"
                    | "--consume-vital-one-test"
                    | "--consume-vital-worker-test"
                    | "--consume-sos-one-test"
                    | "--consume-sos-worker-test"
                    | "--consume-sos-crash-test"
            ) =>
        {
            yihe_next_gateway::consumer::run(
                address.parse()?,
                matches!(
                    mode.as_str(),
                    "--consume-worker-test"
                        | "--consume-vital-worker-test"
                        | "--consume-sos-worker-test"
                ),
                mode.ends_with("-crash-test"),
                if mode.starts_with("--consume-vital-") {
                    yihe_next_gateway::consumer::Lane::Vital
                } else if mode.starts_with("--consume-sos-") {
                    yihe_next_gateway::consumer::Lane::Sos
                } else {
                    yihe_next_gateway::consumer::Lane::Step
                },
            )
            .await
        }
        [_, mode, device] if mode == "--vital-stats-worker-test" => {
            yihe_next_gateway::vitals::stats_worker(device.parse()?).await
        }
        [_, mode, device] if mode == "--vital-stats-test" => {
            yihe_next_gateway::vitals::stats(device.parse()?).await
        }
        [_, mode, address]
            if matches!(
                mode.as_str(),
                "--dispatch-worker-test"
                    | "--dispatch-vital-worker-test"
                    | "--dispatch-sos-worker-test"
            ) =>
        {
            yihe_next_gateway::outbox::dispatch_worker(address.parse()?, dispatch_lane(mode)).await
        }
        [_, mode, address]
            if matches!(
                mode.as_str(),
                "--dispatch-one-test"
                    | "--dispatch-crash-test"
                    | "--dispatch-vital-one-test"
                    | "--dispatch-vital-crash-test"
                    | "--dispatch-sos-one-test"
                    | "--dispatch-sos-crash-test"
            ) =>
        {
            let sent = yihe_next_gateway::outbox::dispatch_one(
                address.parse()?,
                mode.ends_with("-crash-test"),
                dispatch_lane(mode),
            )
            .await?;
            println!("{}", if sent { "published" } else { "empty" });
            Ok(())
        }
        [_, mode, bind]
            if matches!(mode.as_str(), "--native-outbox-test" | "--native-sos-test") =>
        {
            let bind: SocketAddr = bind.parse()?;
            if !bind.ip().is_loopback() {
                return Err("native test listener must be loopback".into());
            }
            let app = if mode == "--native-sos-test" {
                yihe_next_gateway::outbox::sos_router().await?
            } else {
                yihe_next_gateway::outbox::router().await?
            };
            let listener = tokio::net::TcpListener::bind(bind).await?;
            println!("{}", listener.local_addr()?);
            io::stdout().flush()?;
            axum::serve(listener, app).await?;
            Ok(())
        }
        [_, mode, bind] if mode == "--native-step-test" => {
            let bind: SocketAddr = bind.parse()?;
            if !bind.ip().is_loopback() {
                return Err("native test listener must be loopback".into());
            }
            let app = yihe_next_gateway::native::test_router().await?;
            let listener = tokio::net::TcpListener::bind(bind).await?;
            println!("{}", listener.local_addr()?);
            io::stdout().flush()?;
            axum::serve(listener, app).await?;
            Ok(())
        }
        [_, mode, bind, upstream] if mode == "--serve" => {
            let bind: SocketAddr = bind.parse()?;
            if !bind.ip().is_loopback() {
                return Err("experimental listener must be loopback".into());
            }
            let app = router(upstream.parse()?)?;
            let listener = tokio::net::TcpListener::bind(bind).await?;
            println!("{}", listener.local_addr()?);
            io::stdout().flush()?;
            axum::serve(listener, app).await?;
            Ok(())
        }
        _ => Err("usage: yihe-next-gateway --serve LOOPBACK:PORT UPSTREAM:PORT or --oracle".into()),
    }
}
