mod cli;
pub mod query;

fn main() {
    if let Err(error) = cli::run(std::env::args().skip(1)) {
        eprintln!("error: {error}");
        std::process::exit(2);
    }
}
