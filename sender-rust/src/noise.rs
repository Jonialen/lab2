#[derive(Debug, PartialEq, Eq)]
pub struct NoiseResult {
    pub frame_bits: String,
    pub flipped_bits: u64,
    pub flipped_indexes: Vec<usize>,
}

pub fn apply_noise(
    clean_frame_bits: &str,
    numerator: u64,
    denominator: u64,
    seed: u64,
) -> Result<NoiseResult, String> {
    validate_probability(numerator, denominator)?;

    let threshold = if numerator == denominator {
        None
    } else {
        Some(((u128::from(numerator) << 64) / u128::from(denominator)) as u64)
    };
    let mut state = seed;
    let mut noisy = String::with_capacity(clean_frame_bits.len());
    let mut flipped_indexes = Vec::new();

    for (index, bit) in clean_frame_bits.bytes().enumerate() {
        if !matches!(bit, b'0' | b'1') {
            return Err(format!(
                "frame contains a non-bit character at index {index}"
            ));
        }

        // One SplitMix64 draw is consumed for every frame bit, including redundancy bits.
        state = state.wrapping_add(0x9e37_79b9_7f4a_7c15);
        let mut z = state;
        z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
        let draw = z ^ (z >> 31);
        let flip = threshold.is_none_or(|value| draw < value);

        if flip {
            noisy.push(if bit == b'0' { '1' } else { '0' });
            flipped_indexes.push(index);
        } else {
            noisy.push(char::from(bit));
        }
    }

    Ok(NoiseResult {
        frame_bits: noisy,
        flipped_bits: flipped_indexes.len() as u64,
        flipped_indexes,
    })
}

pub fn validate_probability(numerator: u64, denominator: u64) -> Result<(), String> {
    if denominator == 0 || denominator > 1_000_000_000 {
        return Err("probability denominator must be between 1 and 1,000,000,000".to_owned());
    }
    if numerator > denominator {
        return Err("probability numerator must not exceed the denominator".to_owned());
    }
    Ok(())
}
