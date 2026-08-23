use serde::{Deserialize, Serialize};

use crate::{ascii, crc, hamming, noise};

pub const PROTOCOL_VERSION: u8 = 1;
pub const MAX_FRAME_BITS: usize = 1_000_000;
pub const MAX_LINE_BYTES: usize = 1_048_576;

#[derive(Clone, Copy, Debug, Deserialize, PartialEq, Eq, Serialize)]
pub enum Algorithm {
    #[serde(rename = "crc32-iso-hdlc")]
    Crc32IsoHdlc,
    #[serde(rename = "hamming-secded-13-8")]
    HammingSecded13_8,
}

impl Algorithm {
    pub fn parse(value: &str) -> Result<Self, String> {
        match value {
            "crc" | "crc32-iso-hdlc" => Ok(Self::Crc32IsoHdlc),
            "hamming" | "hamming-secded-13-8" => Ok(Self::HammingSecded13_8),
            _ => Err(format!(
                "unsupported algorithm '{value}'; use crc32-iso-hdlc or hamming-secded-13-8"
            )),
        }
    }
}

#[derive(Debug, Deserialize, PartialEq, Eq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct NoiseMetadata {
    pub probability_numerator: u64,
    pub probability_denominator: u64,
    pub seed: u64,
    pub flipped_bits: u64,
}

#[derive(Debug, Deserialize, PartialEq, Eq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub protocol_version: u8,
    pub request_id: String,
    pub algorithm: Algorithm,
    pub source_octets: usize,
    pub frame_bits: String,
    pub noise: NoiseMetadata,
    #[serde(skip)]
    original_message: String,
}

pub fn build_request(
    request_id: &str,
    algorithm: Algorithm,
    message: &str,
    probability_numerator: u64,
    probability_denominator: u64,
    seed: u64,
) -> Result<Request, String> {
    validate_request_id(request_id)?;
    ascii::validate_printable_ascii(message)?;
    noise::validate_probability(probability_numerator, probability_denominator)?;

    let clean_frame = match algorithm {
        Algorithm::Crc32IsoHdlc => crc::encode_frame(message.as_bytes()),
        Algorithm::HammingSecded13_8 => hamming::encode_frame(message.as_bytes()),
    };
    if clean_frame.len() > MAX_FRAME_BITS {
        return Err(format!(
            "encoded frame has {} bits; maximum is {MAX_FRAME_BITS}",
            clean_frame.len()
        ));
    }

    let noisy = noise::apply_noise(
        &clean_frame,
        probability_numerator,
        probability_denominator,
        seed,
    )?;

    Ok(Request {
        protocol_version: PROTOCOL_VERSION,
        request_id: request_id.to_owned(),
        algorithm,
        source_octets: message.len(),
        frame_bits: noisy.frame_bits,
        noise: NoiseMetadata {
            probability_numerator,
            probability_denominator,
            seed,
            flipped_bits: noisy.flipped_bits,
        },
        original_message: message.to_owned(),
    })
}

pub fn validate_request_id(request_id: &str) -> Result<(), String> {
    if !(1..=64).contains(&request_id.len())
        || !request_id
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-'))
    {
        return Err(
            "request ID must contain 1-64 ASCII letters, digits, dots, underscores, or hyphens"
                .to_owned(),
        );
    }
    Ok(())
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Response {
    pub protocol_version: u8,
    #[serde(deserialize_with = "required_nullable")]
    pub request_id: Option<String>,
    pub status: String,
    #[serde(deserialize_with = "required_nullable")]
    pub message: Option<String>,
    #[serde(deserialize_with = "required_nullable")]
    pub error: Option<ResponseError>,
    #[serde(deserialize_with = "required_nullable")]
    pub metrics: Option<Metrics>,
}

fn required_nullable<'de, D, T>(deserializer: D) -> Result<Option<T>, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de>,
{
    Option::<T>::deserialize(deserializer)
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ResponseError {
    pub code: String,
    pub detail: String,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Metrics {
    pub received_bits: u64,
    pub source_bits: u64,
    pub redundancy_bits: u64,
    pub reported_flipped_bits: u64,
    pub detected_units: u64,
    pub corrected_bits: u64,
    pub uncorrectable_units: u64,
}

impl Response {
    pub fn validate(&self, request: &Request) -> Result<(), String> {
        if self.protocol_version != request.protocol_version {
            return Err(format!(
                "response protocol version is {}, expected {}",
                self.protocol_version, request.protocol_version
            ));
        }
        if let Some(request_id) = &self.request_id
            && request_id != &request.request_id
        {
            return Err(format!(
                "response request ID '{request_id}' does not match '{}'",
                request.request_id
            ));
        }

        if request.noise.flipped_bits == 0
            && (self.status != "ok" || self.message.as_deref() != Some(&request.original_message))
        {
            return Err(
                "zero-noise response must be successful and preserve the original message"
                    .to_owned(),
            );
        }

        match self.status.as_str() {
            "ok" | "corrected" => {
                if self.request_id.is_none()
                    || self.message.is_none()
                    || self.error.is_some()
                    || self.metrics.is_none()
                {
                    return Err(
                        "success response has inconsistent message/error/metrics fields".to_owned(),
                    );
                }
                let message = self.message.as_deref().unwrap_or_default();
                ascii::validate_printable_ascii(message)
                    .map_err(|error| format!("response message is invalid: {error}"))?;
                if message.len() != request.source_octets {
                    return Err(format!(
                        "response message has {} octets, expected {}",
                        message.len(),
                        request.source_octets
                    ));
                }
            }
            "detected_uncorrectable" => {
                if self.request_id.is_none()
                    || self.message.is_some()
                    || self.error.is_none()
                    || self.metrics.is_none()
                {
                    return Err(
                        "uncorrectable response has inconsistent message/error/metrics fields"
                            .to_owned(),
                    );
                }
            }
            "invalid_request" => {
                if self.message.is_some() || self.error.is_none() || self.metrics.is_some() {
                    return Err(
                        "invalid-request response has inconsistent message/error/metrics fields"
                            .to_owned(),
                    );
                }
            }
            status => return Err(format!("response contains unknown status '{status}'")),
        }

        let error_code = self.error.as_ref().map(|error| error.code.as_str());
        let valid_error = match self.status.as_str() {
            "ok" | "corrected" => error_code.is_none(),
            "invalid_request" => matches!(
                error_code,
                Some(
                    "invalid_json"
                        | "line_too_long"
                        | "unsupported_version"
                        | "invalid_schema"
                        | "invalid_frame_length"
                )
            ),
            "detected_uncorrectable" => match request.algorithm {
                Algorithm::Crc32IsoHdlc => matches!(
                    error_code,
                    Some("integrity_check_failed" | "invalid_ascii_payload")
                ),
                Algorithm::HammingSecded13_8 => matches!(
                    error_code,
                    Some("uncorrectable_error" | "invalid_ascii_payload")
                ),
            },
            _ => false,
        };
        if !valid_error {
            return Err(format!(
                "response status '{}' is inconsistent with error code {:?}",
                self.status, error_code
            ));
        }

        if let Some(metrics) = &self.metrics {
            validate_metrics(metrics, request, &self.status, error_code)?;
        }

        Ok(())
    }
}

fn validate_metrics(
    metrics: &Metrics,
    request: &Request,
    status: &str,
    error_code: Option<&str>,
) -> Result<(), String> {
    let received_bits = u64::try_from(request.frame_bits.len())
        .map_err(|_| "request frame length cannot be represented as u64".to_owned())?;
    let source_octets = u64::try_from(request.source_octets)
        .map_err(|_| "request source length cannot be represented as u64".to_owned())?;
    let source_bits = source_octets
        .checked_mul(8)
        .ok_or_else(|| "request source bit count overflowed".to_owned())?;
    let redundancy_bits = match request.algorithm {
        Algorithm::Crc32IsoHdlc => 32,
        Algorithm::HammingSecded13_8 => source_octets
            .checked_mul(5)
            .ok_or_else(|| "request redundancy bit count overflowed".to_owned())?,
    };

    let expected = [
        ("received_bits", metrics.received_bits, received_bits),
        ("source_bits", metrics.source_bits, source_bits),
        ("redundancy_bits", metrics.redundancy_bits, redundancy_bits),
        (
            "reported_flipped_bits",
            metrics.reported_flipped_bits,
            request.noise.flipped_bits,
        ),
    ];
    for (name, actual, expected) in expected {
        if actual != expected {
            return Err(format!(
                "response metric {name} is {actual}, expected {expected}"
            ));
        }
    }

    let counters_are = |detected, corrected, uncorrectable| {
        metrics.detected_units == detected
            && metrics.corrected_bits == corrected
            && metrics.uncorrectable_units == uncorrectable
    };
    let counters_valid = match (request.algorithm, status, error_code) {
        (_, "ok", None) => counters_are(0, 0, 0),
        (Algorithm::Crc32IsoHdlc, "detected_uncorrectable", Some("integrity_check_failed")) => {
            counters_are(1, 0, 1)
        }
        (Algorithm::Crc32IsoHdlc, "detected_uncorrectable", Some("invalid_ascii_payload")) => {
            counters_are(0, 0, 0)
        }
        (Algorithm::HammingSecded13_8, "corrected", None) => {
            metrics.corrected_bits > 0
                && metrics.detected_units == metrics.corrected_bits
                && metrics.uncorrectable_units == 0
                && metrics.detected_units <= source_octets
        }
        (Algorithm::HammingSecded13_8, "detected_uncorrectable", Some("uncorrectable_error")) => {
            metrics.uncorrectable_units > 0
                && metrics
                    .corrected_bits
                    .checked_add(metrics.uncorrectable_units)
                    == Some(metrics.detected_units)
                && metrics.detected_units <= source_octets
        }
        (Algorithm::HammingSecded13_8, "detected_uncorrectable", Some("invalid_ascii_payload")) => {
            metrics.uncorrectable_units == 0
                && metrics.detected_units == metrics.corrected_bits
                && metrics.detected_units <= source_octets
        }
        _ => false,
    };
    if !counters_valid {
        return Err(format!(
            "response counters are inconsistent with algorithm {:?}, status '{status}', and error code {error_code:?}",
            request.algorithm
        ));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn request(algorithm: Algorithm, flipped_bits: u64) -> Request {
        Request {
            protocol_version: PROTOCOL_VERSION,
            request_id: "demo-1".to_owned(),
            algorithm,
            source_octets: 1,
            frame_bits: match algorithm {
                Algorithm::Crc32IsoHdlc => "0".repeat(40),
                Algorithm::HammingSecded13_8 => "0".repeat(13),
            },
            noise: NoiseMetadata {
                probability_numerator: 0,
                probability_denominator: 1,
                seed: 0,
                flipped_bits,
            },
            original_message: "A".to_owned(),
        }
    }

    fn response(json: &str) -> Response {
        serde_json::from_str(json).expect("valid response schema")
    }

    #[test]
    fn accepts_a_clean_zero_noise_response() {
        let response: Response = serde_json::from_str(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"ok","message":"A","error":null,"metrics":{"received_bits":13,"source_bits":8,"redundancy_bits":5,"reported_flipped_bits":0,"detected_units":0,"corrected_bits":0,"uncorrectable_units":0}}"#,
        )
        .expect("valid response schema");
        assert_eq!(
            response.validate(&request(Algorithm::HammingSecded13_8, 0)),
            Ok(())
        );
    }

    #[test]
    fn accepts_clean_crc_zero_noise_response() {
        let response = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"ok","message":"A","error":null,"metrics":{"received_bits":40,"source_bits":8,"redundancy_bits":32,"reported_flipped_bits":0,"detected_units":0,"corrected_bits":0,"uncorrectable_units":0}}"#,
        );

        assert_eq!(
            response.validate(&request(Algorithm::Crc32IsoHdlc, 0)),
            Ok(())
        );
    }

    #[test]
    fn rejects_hamming_correction_with_zero_noise() {
        let response = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"corrected","message":"A","error":null,"metrics":{"received_bits":13,"source_bits":8,"redundancy_bits":5,"reported_flipped_bits":0,"detected_units":1,"corrected_bits":1,"uncorrectable_units":0}}"#,
        );

        assert!(
            response
                .validate(&request(Algorithm::HammingSecded13_8, 0))
                .is_err()
        );
    }

    #[test]
    fn rejects_crc_integrity_failure_with_zero_noise() {
        let response = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"detected_uncorrectable","message":null,"error":{"code":"integrity_check_failed","detail":"bad CRC"},"metrics":{"received_bits":40,"source_bits":8,"redundancy_bits":32,"reported_flipped_bits":0,"detected_units":1,"corrected_bits":0,"uncorrectable_units":1}}"#,
        );

        assert!(
            response
                .validate(&request(Algorithm::Crc32IsoHdlc, 0))
                .is_err()
        );
    }

    #[test]
    fn rejects_changed_message_with_zero_noise() {
        let response = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"ok","message":"B","error":null,"metrics":{"received_bits":13,"source_bits":8,"redundancy_bits":5,"reported_flipped_bits":0,"detected_units":0,"corrected_bits":0,"uncorrectable_units":0}}"#,
        );

        assert!(
            response
                .validate(&request(Algorithm::HammingSecded13_8, 0))
                .is_err()
        );
    }

    #[test]
    fn rejects_missing_nullable_fields_and_invalid_semantics() {
        let missing_error = r#"{"protocol_version":1,"request_id":"demo-1","status":"ok","message":"A","metrics":{"received_bits":13,"source_bits":8,"redundancy_bits":5,"reported_flipped_bits":0,"detected_units":0,"corrected_bits":0,"uncorrectable_units":0}}"#;
        assert!(serde_json::from_str::<Response>(missing_error).is_err());

        let unknown_status: Response = serde_json::from_str(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"maybe","message":null,"error":null,"metrics":null}"#,
        )
        .expect("valid schema");
        assert!(
            unknown_status
                .validate(&request(Algorithm::HammingSecded13_8, 0))
                .is_err()
        );
    }

    #[test]
    fn rejects_success_with_impossible_metrics() {
        let response = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"ok","message":"A","error":null,"metrics":{"received_bits":0,"source_bits":0,"redundancy_bits":0,"reported_flipped_bits":999,"detected_units":1,"corrected_bits":1,"uncorrectable_units":1}}"#,
        );

        assert!(
            response
                .validate(&request(Algorithm::HammingSecded13_8, 2))
                .is_err()
        );
    }

    #[test]
    fn rejects_invalid_status_error_combinations() {
        let invalid_request_with_integrity_error = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"invalid_request","message":null,"error":{"code":"integrity_check_failed","detail":"bad"},"metrics":null}"#,
        );
        assert!(
            invalid_request_with_integrity_error
                .validate(&request(Algorithm::Crc32IsoHdlc, 0))
                .is_err()
        );

        let detected_with_schema_error = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"detected_uncorrectable","message":null,"error":{"code":"invalid_schema","detail":"bad"},"metrics":{"received_bits":40,"source_bits":8,"redundancy_bits":32,"reported_flipped_bits":0,"detected_units":1,"corrected_bits":0,"uncorrectable_units":1}}"#,
        );
        assert!(
            detected_with_schema_error
                .validate(&request(Algorithm::Crc32IsoHdlc, 0))
                .is_err()
        );
    }

    #[test]
    fn rejects_algorithm_inconsistent_status_and_counters() {
        let corrected_crc = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"corrected","message":"A","error":null,"metrics":{"received_bits":40,"source_bits":8,"redundancy_bits":32,"reported_flipped_bits":1,"detected_units":1,"corrected_bits":1,"uncorrectable_units":0}}"#,
        );
        assert!(
            corrected_crc
                .validate(&request(Algorithm::Crc32IsoHdlc, 1))
                .is_err()
        );

        let corrected_without_correction = response(
            r#"{"protocol_version":1,"request_id":"demo-1","status":"corrected","message":"A","error":null,"metrics":{"received_bits":13,"source_bits":8,"redundancy_bits":5,"reported_flipped_bits":1,"detected_units":0,"corrected_bits":0,"uncorrectable_units":0}}"#,
        );
        assert!(
            corrected_without_correction
                .validate(&request(Algorithm::HammingSecded13_8, 1))
                .is_err()
        );
    }
}
