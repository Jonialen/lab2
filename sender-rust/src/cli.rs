use std::io::{self, Write};

use crate::protocol::Algorithm;

#[derive(Debug, PartialEq, Eq)]
pub struct Config {
    pub host: String,
    pub port: u16,
    pub message: String,
    pub algorithm: Algorithm,
    pub numerator: u64,
    pub denominator: u64,
    pub seed: u64,
    pub request_id: String,
}

pub fn parse_or_prompt(args: Vec<String>) -> Result<Config, String> {
    if args.len() == 1 {
        return prompt_config();
    }
    parse_args(&args[1..])
}

pub fn parse_args(args: &[String]) -> Result<Config, String> {
    let mut config = Config {
        host: "127.0.0.1".to_owned(),
        port: 9000,
        message: String::new(),
        algorithm: Algorithm::Crc32IsoHdlc,
        numerator: 0,
        denominator: 1,
        seed: 0,
        request_id: "request-1".to_owned(),
    };

    let mut index = 0;
    while index < args.len() {
        let option = &args[index];
        if option == "--help" || option == "-h" {
            return Err(usage());
        }
        let value = args
            .get(index + 1)
            .ok_or_else(|| format!("missing value for {option}\n\n{}", usage()))?;
        match option.as_str() {
            "--host" => config.host = value.clone(),
            "--port" => config.port = parse_number(option, value)?,
            "--message" => config.message = value.clone(),
            "--algorithm" => config.algorithm = Algorithm::parse(value)?,
            "--numerator" => config.numerator = parse_number(option, value)?,
            "--denominator" => config.denominator = parse_number(option, value)?,
            "--seed" => config.seed = parse_number(option, value)?,
            "--request-id" => config.request_id = value.clone(),
            _ => return Err(format!("unknown option '{option}'\n\n{}", usage())),
        }
        index += 2;
    }

    if config.message.is_empty() {
        return Err(format!("--message is required\n\n{}", usage()));
    }
    if config.host.is_empty() {
        return Err("--host must not be empty".to_owned());
    }
    if config.port == 0 {
        return Err("--port must be between 1 and 65535".to_owned());
    }
    Ok(config)
}

fn prompt_config() -> Result<Config, String> {
    println!("Laboratory 2 Rust sender (interactive mode)");
    let message = prompt("Message (printable ASCII)", None)?;
    let algorithm = Algorithm::parse(&prompt("Algorithm (crc or hamming)", Some("crc"))?)?;
    let host = prompt("Receiver host", Some("127.0.0.1"))?;
    let port = parse_number("port", &prompt("Receiver port", Some("9000"))?)?;
    if port == 0 {
        return Err("port must be between 1 and 65535".to_owned());
    }
    let numerator = parse_number(
        "probability numerator",
        &prompt("Noise probability numerator", Some("0"))?,
    )?;
    let denominator = parse_number(
        "probability denominator",
        &prompt("Noise probability denominator", Some("1"))?,
    )?;
    let seed = parse_number("seed", &prompt("Noise seed", Some("0"))?)?;
    let request_id = prompt("Request ID", Some("request-1"))?;

    Ok(Config {
        host,
        port,
        message,
        algorithm,
        numerator,
        denominator,
        seed,
        request_id,
    })
}

fn prompt(label: &str, default: Option<&str>) -> Result<String, String> {
    match default {
        Some(value) => print!("{label} [{value}]: "),
        None => print!("{label}: "),
    }
    io::stdout()
        .flush()
        .map_err(|error| format!("could not write prompt: {error}"))?;

    let mut input = String::new();
    io::stdin()
        .read_line(&mut input)
        .map_err(|error| format!("could not read input: {error}"))?;
    let value = input.trim_end_matches(['\r', '\n']);
    if value.is_empty() {
        default
            .map(str::to_owned)
            .ok_or_else(|| format!("{label} must not be empty"))
    } else {
        Ok(value.to_owned())
    }
}

fn parse_number<T>(name: &str, value: &str) -> Result<T, String>
where
    T: std::str::FromStr,
{
    value
        .parse()
        .map_err(|_| format!("invalid numeric value '{value}' for {name}"))
}

pub fn usage() -> String {
    "Usage: lab2-sender --message TEXT [--algorithm crc|hamming] [--host HOST] [--port PORT] [--numerator N] [--denominator D] [--seed SEED] [--request-id ID]\nRun without arguments for interactive mode.".to_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_reproducible_non_interactive_configuration() {
        let args = [
            "--message",
            "A",
            "--algorithm",
            "hamming",
            "--host",
            "localhost",
            "--port",
            "7000",
            "--numerator",
            "1",
            "--denominator",
            "10",
            "--seed",
            "3",
            "--request-id",
            "case-1",
        ]
        .map(str::to_owned);

        let config = parse_args(&args).expect("valid arguments");
        assert_eq!(config.message, "A");
        assert_eq!(config.algorithm, Algorithm::HammingSecded13_8);
        assert_eq!(config.seed, 3);
        assert_eq!(config.port, 7000);
    }

    #[test]
    fn rejects_unknown_missing_and_invalid_options() {
        assert!(parse_args(&["--unknown".to_owned(), "x".to_owned()]).is_err());
        assert!(parse_args(&["--message".to_owned()]).is_err());
        assert!(
            parse_args(&[
                "--message".to_owned(),
                "A".to_owned(),
                "--port".to_owned(),
                "not-a-port".to_owned(),
            ])
            .is_err()
        );
    }
}
