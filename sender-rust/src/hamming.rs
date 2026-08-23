const DATA_POSITIONS: [usize; 8] = [3, 5, 6, 7, 9, 10, 11, 12];
const PARITY_POSITIONS: [usize; 4] = [1, 2, 4, 8];

#[derive(Debug, PartialEq, Eq)]
pub enum CodewordState {
    Clean,
    Correctable { position: usize },
    Uncorrectable,
}

pub fn encode_frame(bytes: &[u8]) -> String {
    let mut frame = String::with_capacity(bytes.len() * 13);
    for byte in bytes {
        frame.push_str(&encode_byte(*byte));
    }
    frame
}

pub fn encode_byte(byte: u8) -> String {
    // Index zero is unused so array indexes match the protocol's one-based positions.
    let mut positions = [0_u8; 14];

    for (data_index, position) in DATA_POSITIONS.iter().enumerate() {
        positions[*position] = (byte >> (7 - data_index)) & 1;
    }

    for parity_position in PARITY_POSITIONS {
        positions[parity_position] = parity_xor(&positions, parity_position);
    }

    positions[13] = positions[1..13].iter().fold(0, |parity, bit| parity ^ bit);
    positions[1..=13]
        .iter()
        .map(|bit| if *bit == 1 { '1' } else { '0' })
        .collect()
}

pub fn analyze_codeword(codeword: &str) -> Result<(u8, bool, CodewordState), String> {
    if codeword.len() != 13 || !codeword.bytes().all(|byte| matches!(byte, b'0' | b'1')) {
        return Err("Hamming codeword must contain exactly 13 bits".to_owned());
    }

    let mut positions = [0_u8; 14];
    for (index, byte) in codeword.bytes().enumerate() {
        positions[index + 1] = byte - b'0';
    }

    let syndrome = PARITY_POSITIONS
        .iter()
        .filter(|position| parity_xor(&positions, **position) != 0)
        .fold(0_u8, |value, position| value | *position as u8);
    let overall_mismatch = positions[1..=13]
        .iter()
        .fold(0_u8, |parity, bit| parity ^ bit)
        != 0;

    let state = match (syndrome, overall_mismatch) {
        (0, false) => CodewordState::Clean,
        (0, true) => CodewordState::Correctable { position: 13 },
        (1..=12, true) => CodewordState::Correctable {
            position: syndrome as usize,
        },
        _ => CodewordState::Uncorrectable,
    };

    Ok((syndrome, overall_mismatch, state))
}

fn parity_xor(positions: &[u8; 14], parity_position: usize) -> u8 {
    (1..=12)
        .filter(|position| position & parity_position != 0)
        .fold(0, |parity, position| parity ^ positions[position])
}
