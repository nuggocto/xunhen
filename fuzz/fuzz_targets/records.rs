#![no_main]
libfuzzer_sys::fuzz_target!(|input: &[u8]| xunhen::fuzzing::records(input));
