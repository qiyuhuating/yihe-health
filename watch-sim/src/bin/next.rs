//! Explicit opt-in simulator for the experimental signed step-only protocol.
use std::{
    net::SocketAddr,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use yihe_device_wire::{self as wire, Step};

fn seed() -> Result<[u8; 32], Box<dyn std::error::Error>> {
    let text = std::env::var("WATCH_NEXT_SEED_HEX")?;
    if text.len() != 64 || !text.is_ascii() {
        return Err("expected 32-byte hex seed".into());
    }
    let mut seed = [0; 32];
    for (i, b) in seed.iter_mut().enumerate() {
        *b = u8::from_str_radix(&text[i * 2..i * 2 + 2], 16)?;
    }
    Ok(seed)
}
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.as_slice(){
        [mode] if mode=="--public-key"=>{
            for byte in wire::public_key(&seed()?)?{print!("{byte:02x}");}
            println!();
        }
        [mode,addr,device,epoch,request,nonce,observed,steps] if mode=="--send"=>{
            let addr:SocketAddr=addr.parse()?;
            if !addr.ip().is_loopback(){return Err("test simulator requires loopback".into())}
            let now=i64::try_from(SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs())?;
            let step=Step{version:1,device_no:device.clone(),binding_epoch:epoch.parse()?,
                request_id:request.clone(),nonce:nonce.clone(),signed_at:now,observed_at:observed.parse()?,step:Some(steps.parse()?)};
            let raw=wire::sign(&step,&seed()?)?;
            let agent=ureq::AgentBuilder::new().timeout(Duration::from_secs(10)).redirects(0).build();
            let response=match agent.post(&format!("http://{addr}{}",wire::PATH)).set("Content-Type",wire::CONTENT_TYPE).send_bytes(&raw){
                Ok(response)|Err(ureq::Error::Status(_,response))=>response,
                Err(err)=>return Err(err.into()),
            };
            let status=response.status();
            let body=response.into_string()?;
            println!("{status} {body}");
            if status!=202{return Err("admission rejected".into())}
        }
        _=>return Err("usage: watch-sim-next --public-key | --send ADDR DEVICE EPOCH REQUEST_ID NONCE OBSERVED_UNIX STEP; seed via WATCH_NEXT_SEED_HEX".into()),
    }
    Ok(())
}
