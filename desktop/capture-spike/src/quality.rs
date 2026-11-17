//! The 1080p/720p rule of a screen share: at least 30 fps in any case,
//! 1080p (up to 60 fps) while that holds, 720p when it does not. The same
//! steps as the web client (web/src/entities/call/lib/screen-quality.ts),
//! plus one the browser does not need: a hardware encoder sends every frame
//! it is given, so a short link shows as a low bandwidth estimate rather than
//! as lost frames.

/// A moving screen: the capture gives at least this many fps.
const MOVING_FPS: f64 = 30.0;
/// The least a share may send: below it, 720p at full speed beats 1080p stuttering.
const STARVED_FPS: f64 = 30.0;
/// Below this estimate 1080p is a smear even at 30 fps, in bit/s.
const STARVED_BANDWIDTH: f64 = 2_500_000.0;
/// Starved samples in a row before stepping down.
const DOWN_AFTER: u32 = 2;
/// The bandwidth estimate that carries 1080p again, in bit/s.
const ROOM_FOR_1080P: f64 = 6_000_000.0;
/// Samples with room in a row before stepping up, doubled per fall back, up to UP_AFTER_MAX.
const UP_AFTER: u32 = 5;
const UP_AFTER_MAX: u32 = 30;

/// The height the share is sent at.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Level {
    P1080,
    P720,
}

impl Level {
    /// The largest frame of this level, as width and height.
    pub fn bounds(self) -> (u32, u32) {
        match self {
            Level::P1080 => (1920, 1080),
            Level::P720 => (1280, 720),
        }
    }
}

/// One reading of the sender, taken every couple of seconds.
#[derive(Debug, Clone, Copy)]
pub struct Sample {
    /// Frames the capture gave, per second.
    pub source_fps: f64,
    /// Frames that left the encoder, per second.
    pub sent_fps: f64,
    /// The sender's bandwidth estimate in bit/s, when known.
    pub bandwidth: Option<f64>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Quality {
    pub level: Level,
    /// Starved samples in a row (at 1080p).
    starved: u32,
    /// Samples in a row with room for 1080p (at 720p).
    roomy: u32,
    /// How many times the share has stepped down: each makes the way back up slower.
    falls: u32,
}

impl Default for Quality {
    fn default() -> Self {
        Quality { level: Level::P1080, starved: 0, roomy: 0, falls: 0 }
    }
}

impl Quality {
    /// The quality after one more sample.
    pub fn next(self, s: Sample) -> Quality {
        match self.level {
            Level::P1080 => {
                let lost_frames = s.source_fps >= MOVING_FPS && s.sent_fps < STARVED_FPS;
                let short_link = s.bandwidth.is_some_and(|b| b < STARVED_BANDWIDTH);
                let starved = if lost_frames || short_link { self.starved + 1 } else { 0 };
                if starved < DOWN_AFTER {
                    return Quality { starved, ..self };
                }
                Quality { level: Level::P720, starved: 0, roomy: 0, falls: self.falls + 1 }
            }
            Level::P720 => {
                let room = s.bandwidth.is_some_and(|b| b >= ROOM_FOR_1080P);
                let roomy = if room { self.roomy + 1 } else { 0 };
                let needed = (UP_AFTER << (self.falls.saturating_sub(1)).min(8)).min(UP_AFTER_MAX);
                if roomy < needed {
                    return Quality { roomy, ..self };
                }
                Quality { level: Level::P1080, starved: 0, roomy: 0, ..self }
            }
        }
    }
}

/// The size a `width`×`height` source is sent at within `level`: scaled
/// down to fit, never up, aspect kept, both sides even (NV12 needs it).
pub fn fit(width: u32, height: u32, level: Level) -> (u32, u32) {
    let (max_w, max_h) = level.bounds();
    let scale = f64::min(1.0, f64::min(max_w as f64 / width as f64, max_h as f64 / height as f64));
    let even = |v: f64| ((v.round() as u32) & !1).max(2);
    (even(width as f64 * scale), even(height as f64 * scale))
}

#[cfg(test)]
mod tests {
    use super::*;

    const SMOOTH: Sample = Sample { source_fps: 60.0, sent_fps: 60.0, bandwidth: Some(8_000_000.0) };
    const LOSING: Sample = Sample { source_fps: 60.0, sent_fps: 12.0, bandwidth: None };
    const NARROW: Sample = Sample { source_fps: 60.0, sent_fps: 60.0, bandwidth: Some(1_500_000.0) };
    const STILL: Sample = Sample { source_fps: 2.0, sent_fps: 2.0, bandwidth: None };

    fn run(q: Quality, samples: &[Sample]) -> Quality {
        samples.iter().fold(q, |q, s| q.next(*s))
    }

    #[test]
    fn holds_1080p_while_smooth() {
        assert_eq!(run(Quality::default(), &[SMOOTH; 20]).level, Level::P1080);
    }

    #[test]
    fn a_still_screen_is_not_starved() {
        assert_eq!(run(Quality::default(), &[STILL; 20]).level, Level::P1080);
    }

    #[test]
    fn steps_down_after_two_samples_of_lost_frames() {
        assert_eq!(run(Quality::default(), &[LOSING]).level, Level::P1080);
        assert_eq!(run(Quality::default(), &[LOSING, LOSING]).level, Level::P720);
        assert_eq!(run(Quality::default(), &[LOSING, SMOOTH, LOSING]).level, Level::P1080);
    }

    #[test]
    fn steps_down_on_a_narrow_link_even_with_every_frame_sent() {
        assert_eq!(run(Quality::default(), &[NARROW, NARROW]).level, Level::P720);
    }

    #[test]
    fn steps_back_up_slower_after_each_fall() {
        let down = run(Quality::default(), &[LOSING, LOSING]);
        assert_eq!(run(down, &[SMOOTH; 4]).level, Level::P720);
        let up = run(down, &[SMOOTH; 5]);
        assert_eq!(up.level, Level::P1080);
        let down_again = run(up, &[LOSING, LOSING]);
        assert_eq!(run(down_again, &[SMOOTH; 9]).level, Level::P720);
        assert_eq!(run(down_again, &[SMOOTH; 10]).level, Level::P1080);
    }

    #[test]
    fn no_estimate_keeps_720p() {
        let down = run(Quality::default(), &[LOSING, LOSING]);
        assert_eq!(run(down, &[LOSING; 40]).level, Level::P720);
    }

    #[test]
    fn fits_without_upscaling_and_keeps_even_sides() {
        assert_eq!(fit(2560, 1440, Level::P1080), (1920, 1080));
        assert_eq!(fit(3840, 2160, Level::P720), (1280, 720));
        assert_eq!(fit(1280, 1024, Level::P1080), (1280, 1024));
        assert_eq!(fit(801, 601, Level::P1080), (800, 600));
        assert_eq!(fit(3440, 1440, Level::P1080), (1920, 804));
    }
}
