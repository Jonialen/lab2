use crate::ascii::bytes_to_msb_bits;

pub fn crc32_iso_hdlc(bytes: &[u8]) -> u32 {
    let mut crc = 0xffff_ffff_u32;

    for byte in bytes {
        crc ^= u32::from(*byte);
        for _ in 0..8 {
            // 0xEDB88320 is the reflected form of the normative 0x04C11DB7 polynomial.
            let mask = 0_u32.wrapping_sub(crc & 1);
            crc = (crc >> 1) ^ (0xedb8_8320 & mask);
        }
    }

    crc ^ 0xffff_ffff
}

pub fn encode_frame(bytes: &[u8]) -> String {
    let mut frame = bytes_to_msb_bits(bytes);
    let checksum = crc32_iso_hdlc(bytes);
    for shift in (0..32).rev() {
        frame.push(if (checksum >> shift) & 1 == 1 {
            '1'
        } else {
            '0'
        });
    }
    frame
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn matches_standard_check_value() {
        assert_eq!(crc32_iso_hdlc(b"123456789"), 0xcbf4_3926);
    }
}
