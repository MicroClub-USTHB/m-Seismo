//! A `tracing` layer that counts the coordinator's own events by level, so
//! Prometheus can alert on a spike of warnings or errors.
use std::sync::atomic::{AtomicU64, Ordering};
use tracing::{Event, Level, Subscriber};
use tracing_subscriber::{Layer, layer::Context};

// NOTE: process-wide on purpose, there is one subscriber per process
static COUNTS: [AtomicU64; 5] = [const { AtomicU64::new(0) }; 5];
const NAMES: [&str; 5] = ["error", "warn", "info", "debug", "trace"];

fn slot(l: &Level) -> usize {
    match *l {
        Level::ERROR => 0,
        Level::WARN => 1,
        Level::INFO => 2,
        Level::DEBUG => 3,
        Level::TRACE => 4,
    }
}

pub struct LogStats;

impl<S: Subscriber> Layer<S> for LogStats {
    fn on_event(&self, e: &Event<'_>, _: Context<'_, S>) {
        COUNTS[slot(e.metadata().level())].fetch_add(1, Ordering::Relaxed);
    }
}

/// Events seen so far, per level name. Only the ones that passed the filter.
pub fn counts() -> [(&'static str, u64); 5] {
    std::array::from_fn(|i| (NAMES[i], COUNTS[i].load(Ordering::Relaxed)))
}

#[cfg(test)]
mod tests {
    use super::*;
    use tracing_subscriber::prelude::*;

    #[test]
    fn counts_events_by_level() {
        let before = counts();
        tracing::subscriber::with_default(tracing_subscriber::registry().with(LogStats), || {
            tracing::warn!("a");
            tracing::warn!("b");
            tracing::error!("c");
        });
        let after = counts();
        assert_eq!(after[0].1 - before[0].1, 1, "error");
        assert_eq!(after[1].1 - before[1].1, 2, "warn");
    }
}
