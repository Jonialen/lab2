use lab2_sender::ascii;
use lab2_sender::crc;
use lab2_sender::hamming::{self, CodewordState};
use lab2_sender::noise;
use lab2_sender::protocol::{self, Algorithm};
use serde::Deserialize;

#[derive(Deserialize)]
struct Vectors {
    schema_version: u8,
    ascii_vectors: Vec<AsciiVector>,
    crc32_iso_hdlc_vectors: Vec<CrcVector>,
    hamming_secded_13_8_vectors: Vec<HammingVector>,
    deterministic_noise_vectors: Vec<NoiseVector>,
}

#[derive(Deserialize)]
struct AsciiVector {
    input_text: String,
    expected_octets_decimal: Vec<u8>,
    expected_bits_most_significant_first: String,
}

#[derive(Deserialize)]
struct CrcVector {
    input_text: String,
    expected_crc_decimal: u32,
    expected_clean_frame_bits: String,
}

#[derive(Deserialize)]
struct HammingVector {
    input_text: String,
    expected_clean_codeword_positions_1_through_13: String,
    received_codeword_positions_1_through_13: String,
    expected_syndrome: u8,
    expected_overall_parity_mismatch: u8,
    expected_receiver_status: String,
    introduced_error_bit_positions_one_based: Option<Vec<usize>>,
}

#[derive(Deserialize)]
struct NoiseVector {
    input_clean_frame_bits: String,
    probability_numerator: u64,
    probability_denominator: u64,
    seed: u64,
    expected_flipped_zero_based_frame_indexes: Vec<usize>,
    expected_flipped_bits_count: u64,
    expected_noisy_frame_bits: String,
}

fn vectors() -> Vectors {
    serde_json::from_str(include_str!("../../protocol/test-vectors.json"))
        .expect("normative vector file must be valid JSON")
}

#[test]
fn matches_normative_ascii_and_crc_vectors() {
    let vectors = vectors();
    assert_eq!(vectors.schema_version, 1);

    for vector in vectors.ascii_vectors {
        assert_eq!(vector.input_text.as_bytes(), vector.expected_octets_decimal);
        assert_eq!(
            ascii::bytes_to_msb_bits(vector.input_text.as_bytes()),
            vector.expected_bits_most_significant_first
        );
    }
    for vector in vectors.crc32_iso_hdlc_vectors {
        assert_eq!(
            crc::crc32_iso_hdlc(vector.input_text.as_bytes()),
            vector.expected_crc_decimal
        );
        assert_eq!(
            crc::encode_frame(vector.input_text.as_bytes()),
            vector.expected_clean_frame_bits
        );
    }
}

#[test]
fn matches_normative_hamming_clean_single_and_double_error_vectors() {
    for vector in vectors().hamming_secded_13_8_vectors {
        assert_eq!(
            hamming::encode_frame(vector.input_text.as_bytes()),
            vector.expected_clean_codeword_positions_1_through_13
        );
        let (syndrome, mismatch, state) =
            hamming::analyze_codeword(&vector.received_codeword_positions_1_through_13)
                .expect("normative codeword");
        assert_eq!(syndrome, vector.expected_syndrome);
        assert_eq!(u8::from(mismatch), vector.expected_overall_parity_mismatch);
        let expected_state = match vector.expected_receiver_status.as_str() {
            "ok" => CodewordState::Clean,
            "corrected" => CodewordState::Correctable {
                position: vector.introduced_error_bit_positions_one_based.unwrap()[0],
            },
            "detected_uncorrectable" => CodewordState::Uncorrectable,
            status => panic!("unexpected normative status {status}"),
        };
        assert_eq!(state, expected_state);
    }
}

#[test]
fn matches_normative_splitmix64_noise_vector() {
    for vector in vectors().deterministic_noise_vectors {
        let result = noise::apply_noise(
            &vector.input_clean_frame_bits,
            vector.probability_numerator,
            vector.probability_denominator,
            vector.seed,
        )
        .expect("normative noise parameters");
        assert_eq!(result.frame_bits, vector.expected_noisy_frame_bits);
        assert_eq!(result.flipped_bits, vector.expected_flipped_bits_count);
        assert_eq!(
            result.flipped_indexes,
            vector.expected_flipped_zero_based_frame_indexes
        );
    }
}

#[test]
fn validates_input_and_builds_exact_request() {
    let request = protocol::build_request("demo-1", Algorithm::HammingSecded13_8, "A", 0, 1, 42)
        .expect("valid request");
    assert_eq!(
        serde_json::to_string(&request).expect("serializable request"),
        r#"{"protocol_version":1,"request_id":"demo-1","algorithm":"hamming-secded-13-8","source_octets":1,"frame_bits":"1000100100010","noise":{"probability_numerator":0,"probability_denominator":1,"seed":42,"flipped_bits":0}}"#
    );

    assert!(protocol::build_request("bad id", Algorithm::Crc32IsoHdlc, "A", 0, 1, 0).is_err());
    assert!(protocol::build_request("ok", Algorithm::Crc32IsoHdlc, "\n", 0, 1, 0).is_err());
    assert!(protocol::build_request("ok", Algorithm::Crc32IsoHdlc, "A", 2, 1, 0).is_err());
}
