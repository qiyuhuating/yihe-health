//! Shared experimental step-only wire contract. No firmware or transport dependencies.
use prost::Message;
use ring::signature::{self, Ed25519KeyPair, KeyPair};

const DOMAIN: &[u8] = b"yihe-next-step-v1\0";
pub const MAX_PACKET: usize = 1024;
pub const PATH: &str = "/api/v2/device/step";
pub const CONTENT_TYPE: &str = "application/x-protobuf";

#[derive(Clone, PartialEq, Message)]
pub struct Step {
    #[prost(uint32, tag = "1")]
    pub version: u32,
    #[prost(string, tag = "2")]
    pub device_no: String,
    #[prost(int64, tag = "3")]
    pub binding_epoch: i64,
    #[prost(string, tag = "4")]
    pub request_id: String,
    #[prost(string, tag = "5")]
    pub nonce: String,
    #[prost(int64, tag = "6")]
    pub signed_at: i64,
    #[prost(int64, tag = "7")]
    pub observed_at: i64,
    #[prost(uint32, optional, tag = "8")]
    pub step: Option<u32>,
}
#[derive(Clone, PartialEq, Message)]
pub struct SignedStep {
    #[prost(bytes = "vec", tag = "1")]
    pub payload: Vec<u8>,
    #[prost(bytes = "vec", tag = "2")]
    pub signature: Vec<u8>,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    Invalid,
    Signature,
}
impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "invalid device packet: {self:?}")
    }
}
impl std::error::Error for Error {}

impl Step {
    pub fn validate(&self, now: i64) -> Result<(), Error> {
        if self.version != 1
            || self.device_no.is_empty()
            || self.device_no.len() > 32
            || !self
                .device_no
                .bytes()
                .all(|b| b.is_ascii_uppercase() || b.is_ascii_digit() || b == b'-' || b == b'_')
            || self.binding_epoch <= 0
            || self.request_id.is_empty()
            || self.request_id.len() > 64
            || !self
                .request_id
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_.:-".contains(&b))
            || !(16..=64).contains(&self.nonce.len())
            || !self
                .nonce
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
            || self.step.is_none_or(|n| n > 200000)
            || self.observed_at <= 0
            || self.observed_at > now.saturating_add(300)
        {
            return Err(Error::Invalid);
        }
        if self.signed_at.abs_diff(now) > 300 {
            return Err(Error::Signature);
        }
        Ok(())
    }
}
fn signing_bytes(payload: &[u8]) -> Vec<u8> {
    let mut data = Vec::with_capacity(DOMAIN.len() + payload.len());
    data.extend_from_slice(DOMAIN);
    data.extend_from_slice(payload);
    data
}
pub fn public_key(seed: &[u8; 32]) -> Result<[u8; 32], Error> {
    let key = Ed25519KeyPair::from_seed_unchecked(seed).map_err(|_| Error::Signature)?;
    key.public_key()
        .as_ref()
        .try_into()
        .map_err(|_| Error::Signature)
}
pub fn sign(step: &Step, seed: &[u8; 32]) -> Result<Vec<u8>, Error> {
    step.validate(step.signed_at)?;
    let key = Ed25519KeyPair::from_seed_unchecked(seed).map_err(|_| Error::Signature)?;
    let payload = step.encode_to_vec();
    let signature = key.sign(&signing_bytes(&payload)).as_ref().to_vec();
    Ok(SignedStep { payload, signature }.encode_to_vec())
}
/// Canonical byte equality rejects duplicate fields, unknown fields, nonminimal
/// varints and alternative orderings. Never authenticate a decoded approximation.
pub fn decode(raw: &[u8], now: i64) -> Result<(Step, SignedStep), Error> {
    if raw.is_empty() || raw.len() > MAX_PACKET {
        return Err(Error::Invalid);
    }
    let envelope = SignedStep::decode(raw).map_err(|_| Error::Invalid)?;
    if envelope.encode_to_vec() != raw || envelope.signature.len() != 64 {
        return Err(Error::Invalid);
    }
    let step = Step::decode(envelope.payload.as_slice()).map_err(|_| Error::Invalid)?;
    if step.encode_to_vec() != envelope.payload {
        return Err(Error::Invalid);
    }
    step.validate(now)?;
    Ok((step, envelope))
}
pub fn verify(envelope: &SignedStep, key: &[u8]) -> Result<(), Error> {
    if key.len() != 32 {
        return Err(Error::Signature);
    }
    signature::UnparsedPublicKey::new(&signature::ED25519, key)
        .verify(&signing_bytes(&envelope.payload), &envelope.signature)
        .map_err(|_| Error::Signature)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn signed_canonical_packet() {
        let step = Step {
            version: 1,
            device_no: "WATCH001".into(),
            binding_epoch: 1,
            request_id: "fixture-1".into(),
            nonce: "fixture-nonce-0001".into(),
            signed_at: 1700000000,
            observed_at: 1700000000,
            step: Some(0),
        };
        let seed = [7; 32];
        let raw = sign(&step, &seed).unwrap();
        let (decoded, envelope) = decode(&raw, step.signed_at).unwrap();
        assert_eq!(decoded, step);
        assert!(verify(&envelope, &public_key(&seed).unwrap()).is_ok());
        assert_eq!(
            verify(&envelope, &public_key(&[8; 32]).unwrap()),
            Err(Error::Signature)
        );
        let mut tampered = envelope.clone();
        tampered.signature[0] ^= 1;
        assert_eq!(
            verify(&tampered, &public_key(&seed).unwrap()),
            Err(Error::Signature)
        );
        let mut extra = raw.clone();
        extra.extend([0x18, 0x01]);
        assert!(decode(&extra, step.signed_at).is_err());
        assert!(decode(&raw, step.signed_at + 301).is_err());
        let mut missing = step;
        missing.step = None;
        assert!(sign(&missing, &seed).is_err());
    }
}
