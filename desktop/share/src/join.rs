//! Getting into a call the way the web client does: sign in, then ask the
//! API for a LiveKit token of the channel. As its own participant that is
//! POST /channels/{id}/voice; as the screen companion of someone already in
//! the call (the desktop client next to their browser) it is
//! POST /channels/{id}/voice/screen, whose token may only publish the
//! screen share and joins as `<user id>.screen`.

use anyhow::{Context, Result, bail};
use serde::Deserialize;

/// Where to connect and with what.
pub struct Ticket {
    pub url: String,
    pub token: String,
}

/// How the CLI joins the call.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Role {
    /// A participant of its own (a separate account from the browser's).
    Participant,
    /// The screen of the same user, who must already be in the call.
    Companion,
}

/// The API path that issues the token for `role` in `channel`.
fn token_path(channel: &str, role: Role) -> String {
    match role {
        Role::Participant => format!("/api/v1/channels/{channel}/voice"),
        Role::Companion => format!("/api/v1/channels/{channel}/voice/screen"),
    }
}

/// The parts of an RFC 7807 problem the CLI reports.
#[derive(Deserialize)]
struct Problem {
    code: String,
    #[serde(default)]
    detail: Option<String>,
}

#[derive(Deserialize)]
struct VoiceJoin {
    token: String,
    url: String,
}

/// Signs in to the instance at `origin` (as the browser sees it, e.g.
/// http://localhost:4173) and returns the token for `channel` as `role`. The
/// API wants the Origin header on writes and the session cookie on
/// everything else. Signing in opens a session of its own, so a companion
/// leaves the browser's session alone.
pub fn join(origin: &str, username: &str, password: &str, channel: &str, role: Role) -> Result<Ticket> {
    let origin = origin.trim_end_matches('/');
    let agent: ureq::Agent = ureq::Agent::config_builder().http_status_as_error(false).build().into();
    let res = agent
        .post(format!("{origin}/api/v1/auth/login"))
        .header("Origin", origin)
        .send_json(serde_json::json!({ "username": username, "password": password }))
        .context("join: sign in")?;
    if !res.status().is_success() {
        bail!("join: sign in: {}", problem(res));
    }
    let session = res
        .headers()
        .get_all("set-cookie")
        .iter()
        .filter_map(|v| v.to_str().ok())
        .find_map(|v| v.split(';').next().filter(|c| c.starts_with("zz_session=")))
        .map(str::to_owned);
    let Some(session) = session else { bail!("join: the sign-in set no zz_session cookie") };

    let mut res = agent
        .post(format!("{origin}{}", token_path(channel, role)))
        .header("Origin", origin)
        .header("Cookie", &session)
        .send_empty()
        .context("join: get a voice token")?;
    if res.status() == 409 && role == Role::Companion {
        bail!("join: {}; join the voice channel in the browser first", problem(res));
    }
    if !res.status().is_success() {
        bail!("join: get a voice token: {}", problem(res));
    }
    let join: VoiceJoin = res.body_mut().read_json().context("join: read the voice token")?;
    Ok(Ticket { url: join.url, token: join.token })
}

/// The status and problem code of a refused request, e.g. "409 not_in_voice".
fn problem(mut res: ureq::http::Response<ureq::Body>) -> String {
    let status = res.status().as_u16();
    match res.body_mut().read_json::<Problem>() {
        Ok(p) => match p.detail {
            Some(detail) => format!("{status} {} ({detail})", p.code),
            None => format!("{status} {}", p.code),
        },
        Err(_) => status.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_companion_asks_for_the_screen_token() {
        assert_eq!(token_path("42", Role::Participant), "/api/v1/channels/42/voice");
        assert_eq!(token_path("42", Role::Companion), "/api/v1/channels/42/voice/screen");
    }
}
