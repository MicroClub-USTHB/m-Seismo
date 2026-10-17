mod cli;
pub mod config;
pub mod model;
pub mod query;
pub mod redaction;
pub mod source;

fn main() {
    if let Err(error) = cli::run(std::env::args().skip(1)) {
        eprintln!("error: {error}");
        std::process::exit(2);
    }
}
