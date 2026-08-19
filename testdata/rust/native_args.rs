#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo<'_>) -> ! {
    loop {
        core::hint::spin_loop();
    }
}

#[no_mangle]
pub extern "C" fn ReflektorRustArgs0() -> usize {
    0x1234
}

#[no_mangle]
pub extern "C" fn ReflektorRustArgs1(a0: usize) -> usize {
    a0 ^ 0x55
}

#[no_mangle]
pub extern "C" fn ReflektorRustArgs2(a0: usize, a1: usize) -> usize {
    a0.wrapping_add(a1.wrapping_mul(3)).wrapping_add(7)
}

#[no_mangle]
pub extern "C" fn ReflektorRustArgs3(a0: usize, a1: usize, a2: usize) -> usize {
    a0.wrapping_add(a1.wrapping_mul(3))
        .wrapping_add(a2.wrapping_mul(5))
        .wrapping_add(11)
}
