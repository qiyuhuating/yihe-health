use serde_json::{Value, json};
use std::{
    io::{self, BufRead, Write},
    net::SocketAddr,
};
use yihe_next_gateway::{core_statistics, router, sos_shape};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    match args.as_slice() {
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
                "--consume-one-test" | "--consume-worker-test" | "--consume-crash-test"
            ) =>
        {
            yihe_next_gateway::consumer::run(
                address.parse()?,
                mode == "--consume-worker-test",
                mode == "--consume-crash-test",
            )
            .await
        }
        [_, mode, address] if mode == "--dispatch-worker-test" => {
            yihe_next_gateway::outbox::dispatch_worker(address.parse()?).await
        }
        [_, mode, address] if mode == "--dispatch-one-test" || mode == "--dispatch-crash-test" => {
            let sent = yihe_next_gateway::outbox::dispatch_one(
                address.parse()?,
                mode == "--dispatch-crash-test",
            )
            .await?;
            println!("{}", if sent { "published" } else { "empty" });
            Ok(())
        }
        [_, mode, bind] if mode == "--native-outbox-test" => {
            let bind: SocketAddr = bind.parse()?;
            if !bind.ip().is_loopback() {
                return Err("native test listener must be loopback".into());
            }
            let app = yihe_next_gateway::outbox::router().await?;
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
