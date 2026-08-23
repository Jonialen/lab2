use std::io::{BufRead, BufReader, Read, Write};
use std::net::{Shutdown, TcpStream, ToSocketAddrs};
use std::process::ExitCode;
use std::time::{Duration, Instant};

use lab2_sender::cli;
use lab2_sender::protocol::{self, Response};

const SOCKET_TIMEOUT: Duration = Duration::from_secs(10);

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("Error: {error}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = std::env::args().collect();
    if args
        .iter()
        .skip(1)
        .any(|argument| matches!(argument.as_str(), "--help" | "-h"))
    {
        println!("{}", cli::usage());
        return Ok(());
    }
    let config = cli::parse_or_prompt(args)?;
    let request = protocol::build_request(
        &config.request_id,
        config.algorithm,
        &config.message,
        config.numerator,
        config.denominator,
        config.seed,
    )?;
    let request_line = serde_json::to_string(&request)
        .map_err(|error| format!("could not serialize request: {error}"))?;

    let address = format!("{}:{}", config.host, config.port);
    let mut stream = connect_with_timeout(&address, SOCKET_TIMEOUT)?;
    stream
        .set_read_timeout(Some(SOCKET_TIMEOUT))
        .map_err(|error| format!("could not configure response read timeout: {error}"))?;
    stream
        .set_write_timeout(Some(SOCKET_TIMEOUT))
        .map_err(|error| format!("could not configure request write timeout: {error}"))?;
    stream
        .write_all(format!("{request_line}\n").as_bytes())
        .map_err(|error| format!("could not send request: {error}"))?;
    stream
        .shutdown(Shutdown::Write)
        .map_err(|error| format!("could not finish request: {error}"))?;

    let mut response_line = String::new();
    let bytes_read = BufReader::new(stream)
        .take((protocol::MAX_LINE_BYTES + 2) as u64)
        .read_line(&mut response_line)
        .map_err(|error| format!("could not read response: {error}"))?;
    if bytes_read == 0 {
        return Err("receiver closed the connection without a response".to_owned());
    }
    if !response_line.ends_with('\n') {
        return Err("receiver response is not LF-terminated or exceeds the line limit".to_owned());
    }
    if response_line.len() - 1 > protocol::MAX_LINE_BYTES {
        return Err("receiver response exceeds the protocol line limit".to_owned());
    }

    let response: Response = serde_json::from_str(response_line.trim_end_matches('\n'))
        .map_err(|error| format!("receiver returned invalid JSON/schema: {error}"))?;
    response.validate(&request)?;
    print_response(&response);
    Ok(())
}

fn connect_with_timeout(address: &str, timeout: Duration) -> Result<TcpStream, String> {
    let started = Instant::now();
    let socket_addresses = address
        .to_socket_addrs()
        .map_err(|error| format!("could not resolve receiver address {address}: {error}"))?;
    let mut last_error = None;
    for socket_address in socket_addresses {
        let Some(remaining) = timeout.checked_sub(started.elapsed()) else {
            break;
        };
        match TcpStream::connect_timeout(&socket_address, remaining) {
            Ok(stream) => return Ok(stream),
            Err(error) => last_error = Some(error),
        }
    }
    let detail = last_error
        .map(|error| error.to_string())
        .unwrap_or_else(|| "no address completed before the timeout".to_owned());
    Err(format!(
        "could not connect to {address} within {timeout:?}: {detail}"
    ))
}

fn print_response(response: &Response) {
    println!("Status: {}", response.status);
    if let Some(message) = &response.message {
        println!("Message: {message}");
    }
    if let Some(error) = &response.error {
        println!("Receiver error: {} — {}", error.code, error.detail);
    }
    if let Some(metrics) = &response.metrics {
        println!(
            "Metrics: received={} bits, source={} bits, redundancy={} bits, reported_flips={}, detected_units={}, corrected_bits={}, uncorrectable_units={}",
            metrics.received_bits,
            metrics.source_bits,
            metrics.redundancy_bits,
            metrics.reported_flipped_bits,
            metrics.detected_units,
            metrics.corrected_bits,
            metrics.uncorrectable_units
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;

    #[test]
    fn configures_read_and_write_timeouts() {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind test listener");
        let address = listener.local_addr().expect("listener address");
        let acceptor = std::thread::spawn(move || listener.accept().expect("accept connection"));

        let stream = connect_with_timeout(&address.to_string(), Duration::from_secs(1))
            .expect("connect to test listener");
        stream
            .set_read_timeout(Some(SOCKET_TIMEOUT))
            .expect("set read timeout");
        stream
            .set_write_timeout(Some(SOCKET_TIMEOUT))
            .expect("set write timeout");
        assert_eq!(
            stream.read_timeout().expect("read timeout"),
            Some(SOCKET_TIMEOUT)
        );
        assert_eq!(
            stream.write_timeout().expect("write timeout"),
            Some(SOCKET_TIMEOUT)
        );

        drop(stream);
        acceptor.join().expect("join acceptor");
    }

    #[test]
    fn connection_error_names_target_and_timeout() {
        let error = connect_with_timeout("127.0.0.1:0", Duration::from_millis(50))
            .expect_err("port zero must not connect");
        assert!(error.contains("could not connect to 127.0.0.1:0 within 50ms"));
    }
}
