pub fn validate_printable_ascii(text: &str) -> Result<(), String> {
    if text.is_empty() {
        return Err("message must not be empty".to_owned());
    }

    if let Some((index, byte)) = text
        .bytes()
        .enumerate()
        .find(|(_, byte)| !(0x20..=0x7e).contains(byte))
    {
        return Err(format!(
            "message byte at index {index} is 0x{byte:02X}; expected printable ASCII (0x20..0x7E)"
        ));
    }

    Ok(())
}

pub fn bytes_to_msb_bits(bytes: &[u8]) -> String {
    let mut bits = String::with_capacity(bytes.len() * 8);
    for byte in bytes {
        for shift in (0..8).rev() {
            bits.push(if (byte >> shift) & 1 == 1 { '1' } else { '0' });
        }
    }
    bits
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_empty_and_non_printable_input() {
        assert!(validate_printable_ascii("").is_err());
        assert!(validate_printable_ascii("line\n").is_err());
        assert!(validate_printable_ascii("é").is_err());
    }

    #[test]
    fn accepts_printable_ascii_boundaries() {
        assert_eq!(validate_printable_ascii(" ~"), Ok(()));
    }
}
