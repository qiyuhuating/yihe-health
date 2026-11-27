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
                    Some("core") => core_statistics(serde_json::from_value(input["body"].clone())?)
                        .unwrap_or_else(|e| json!({"error":e})),
                    _ => return Err("unknown oracle case".into()),
                };
                writeln!(stdout, "{result}")?;
                stdout.flush()?;
            }
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
