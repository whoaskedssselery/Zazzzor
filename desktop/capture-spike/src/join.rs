//! Getting into a call the way the web client does: sign in, then ask the
//! API for a LiveKit token of the channel (POST /channels/{id}/voice).

use anyhow::{Context, Result, bail};
use serde::Deserialize;

/// Where to connect and with what.
pub struct Ticket {
    pub url: String,
    pub token: String,
}

#[derive(Deserialize)]
struct VoiceJoin {
    token: String,
    url: String,
}

/// Signs in to the instance at `origin` (as the browser sees it, e.g.
/// http://localhost:4173) and returns the token for `channel`. The API wants
/// the Origin header on writes and the session cookie on everything else.
pub fn join(origin: &str, username: &str, password: &str, channel: &str) -> Result<Ticket> {
    let origin = origin.trim_end_matches('/');
    let agent = ureq::Agent::new_with_defaults();
    let res = agent
        .post(format!("{origin}/api/v1/auth/login"))
        .header("Origin", origin)
        .send_json(serde_json::json!({ "username": username, "password": password }))
        .context("join: sign in")?;
    let session = res
        .headers()
        .get_all("set-cookie")
        .iter()
        .filter_map(|v| v.to_str().ok())
        .find_map(|v| v.split(';').next().filter(|c| c.starts_with("zz_session=")))
        .map(str::to_owned);
    let Some(session) = session else { bail!("join: the sign-in set no zz_session cookie") };

    let join: VoiceJoin = agent
        .post(format!("{origin}/api/v1/channels/{channel}/voice"))
        .header("Origin", origin)
        .header("Cookie", &session)
        .send_empty()
        .context("join: get a voice token")?
        .body_mut()
        .read_json()
        .context("join: read the voice token")?;
    Ok(Ticket { url: join.url, token: join.token })
}
