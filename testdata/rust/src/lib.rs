#![no_std]

#[cfg(all(target_os = "macos", not(target_pointer_width = "64")))]
compile_error!("the macOS fixture supports only 64-bit Apple targets");

use core::panic::PanicInfo;

const MARKER_OK: &[u8] = b"ok:200";

#[panic_handler]
fn panic(_info: &PanicInfo<'_>) -> ! {
    loop {
        core::hint::spin_loop();
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn StartW() {
    #[cfg(target_os = "macos")]
    macos_thread::run();

    #[cfg(not(target_os = "macos"))]
    request_and_record();
}

fn request_and_record() {
    let result = platform::get_example_com();
    platform::write_marker(match result {
        Ok(()) => MARKER_OK,
        Err(stage) => stage,
    });
}

#[cfg(target_os = "macos")]
mod macos_thread {
    use core::ffi::{c_int, c_long, c_void};
    use core::mem::MaybeUninit;
    use core::ptr;

    const REQUEST_STACK_SIZE: usize = 8 * 1024 * 1024;

    // Darwin's 64-bit pthread_attr_t is an opaque long followed by 56 bytes.
    // Both supported Apple targets are 64-bit.
    #[repr(C)]
    struct PthreadAttr {
        signature: c_long,
        opaque: [u8; 56],
    }

    type Pthread = *mut c_void;

    const _: [(); 64] = [(); core::mem::size_of::<PthreadAttr>()];
    const _: [(); 8] = [(); core::mem::align_of::<PthreadAttr>()];

    #[link(name = "c")]
    unsafe extern "C" {
        fn pthread_attr_init(attr: *mut PthreadAttr) -> c_int;
        fn pthread_attr_setstacksize(attr: *mut PthreadAttr, stack_size: usize) -> c_int;
        fn pthread_attr_destroy(attr: *mut PthreadAttr) -> c_int;
        fn pthread_create(
            thread: *mut Pthread,
            attr: *const PthreadAttr,
            start: unsafe extern "C" fn(*mut c_void) -> *mut c_void,
            argument: *mut c_void,
        ) -> c_int;
        fn pthread_join(thread: Pthread, result: *mut *mut c_void) -> c_int;
    }

    unsafe extern "C" fn request_worker(_argument: *mut c_void) -> *mut c_void {
        super::request_and_record();
        ptr::null_mut()
    }

    pub fn run() {
        // Go-created secondary threads have a comparatively small native stack
        // on Intel macOS. libcurl may exhaust it, so execute the request on a
        // joined pthread with the same stack size as a macOS main thread.
        unsafe {
            let mut attr = MaybeUninit::<PthreadAttr>::uninit();
            if pthread_attr_init(attr.as_mut_ptr()) != 0 {
                super::platform::write_marker(b"error:pthread-attr-init");
                return;
            }

            let attr = attr.as_mut_ptr();
            if pthread_attr_setstacksize(attr, REQUEST_STACK_SIZE) != 0 {
                let _ = pthread_attr_destroy(attr);
                super::platform::write_marker(b"error:pthread-stack-size");
                return;
            }

            let mut thread = MaybeUninit::<Pthread>::uninit();
            let create_result =
                pthread_create(thread.as_mut_ptr(), attr, request_worker, ptr::null_mut());
            let _ = pthread_attr_destroy(attr);
            if create_result != 0 {
                super::platform::write_marker(b"error:pthread-create");
                return;
            }

            if pthread_join(thread.assume_init(), ptr::null_mut()) != 0 {
                super::platform::write_marker(b"error:pthread-join");
            }
        }
    }
}

#[cfg(any(target_os = "linux", target_os = "macos"))]
mod platform {
    use core::ffi::{c_char, c_int, c_long, c_uint, c_void};
    use core::ptr;

    const CURL_GLOBAL_DEFAULT: c_long = 3;
    const CURLE_OK: c_int = 0;
    const CURLOPT_WRITEDATA: c_uint = 10_001;
    const CURLOPT_URL: c_uint = 10_002;
    const CURLOPT_USERAGENT: c_uint = 10_018;
    const CURLOPT_WRITEFUNCTION: c_uint = 20_011;
    const CURLOPT_TIMEOUT: c_uint = 13;
    const CURLOPT_FOLLOWLOCATION: c_uint = 52;
    const CURLOPT_CONNECTTIMEOUT: c_uint = 78;
    const CURLOPT_NOSIGNAL: c_uint = 99;
    const CURLINFO_RESPONSE_CODE: c_uint = 0x20_0002;

    static URL: &[u8] = b"https://example.com/\0";
    static USER_AGENT: &[u8] = b"reflektor-rust-fixture/1.0\0";
    static MARKER_ENV: &[u8] = b"REFLEKTOR_MARKER\0";
    static DEFAULT_MARKER: &[u8] = b"/tmp/reflektor_rust_marker.txt\0";
    static WRITE_MODE: &[u8] = b"wb\0";

    #[link(name = "curl")]
    unsafe extern "C" {
        fn curl_global_init(flags: c_long) -> c_int;
        fn curl_easy_init() -> *mut c_void;
        fn curl_easy_setopt(handle: *mut c_void, option: c_uint, ...) -> c_int;
        fn curl_easy_perform(handle: *mut c_void) -> c_int;
        fn curl_easy_getinfo(handle: *mut c_void, info: c_uint, ...) -> c_int;
        fn curl_easy_cleanup(handle: *mut c_void);
    }

    #[link(name = "c")]
    unsafe extern "C" {
        fn getenv(name: *const c_char) -> *mut c_char;
        fn fopen(path: *const c_char, mode: *const c_char) -> *mut c_void;
        fn fwrite(ptr: *const c_void, size: usize, count: usize, stream: *mut c_void) -> usize;
        fn fclose(stream: *mut c_void) -> c_int;
    }

    unsafe extern "C" fn count_body(
        _data: *mut u8,
        size: usize,
        count: usize,
        user_data: *mut c_void,
    ) -> usize {
        let Some(total) = size.checked_mul(count) else {
            return 0;
        };
        if total != 0 && !user_data.is_null() {
            let counter = user_data.cast::<usize>();
            // SAFETY: libcurl receives the address of body_bytes below and only
            // invokes this callback before curl_easy_perform returns.
            unsafe {
                *counter = (*counter).saturating_add(total);
            }
        }
        total
    }

    pub fn get_example_com() -> Result<(), &'static [u8]> {
        // SAFETY: every pointer passed to libcurl remains live until the easy
        // handle is cleaned up, and all option values match libcurl's C ABI.
        unsafe {
            if curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK {
                return Err(b"error:curl-global-init");
            }

            let handle = curl_easy_init();
            if handle.is_null() {
                return Err(b"error:curl-easy-init");
            }

            let mut body_bytes = 0usize;
            let callback = count_body as *const () as *const c_void;
            let options_ok = curl_easy_setopt(handle, CURLOPT_URL, URL.as_ptr()) == CURLE_OK
                && curl_easy_setopt(handle, CURLOPT_USERAGENT, USER_AGENT.as_ptr()) == CURLE_OK
                && curl_easy_setopt(handle, CURLOPT_FOLLOWLOCATION, 1 as c_long) == CURLE_OK
                && curl_easy_setopt(handle, CURLOPT_CONNECTTIMEOUT, 10 as c_long) == CURLE_OK
                && curl_easy_setopt(handle, CURLOPT_TIMEOUT, 20 as c_long) == CURLE_OK
                && curl_easy_setopt(handle, CURLOPT_NOSIGNAL, 1 as c_long) == CURLE_OK
                && curl_easy_setopt(handle, CURLOPT_WRITEFUNCTION, callback) == CURLE_OK
                && curl_easy_setopt(
                    handle,
                    CURLOPT_WRITEDATA,
                    ptr::addr_of_mut!(body_bytes).cast::<c_void>(),
                ) == CURLE_OK;
            if !options_ok {
                curl_easy_cleanup(handle);
                return Err(b"error:curl-setopt");
            }

            if curl_easy_perform(handle) != CURLE_OK {
                curl_easy_cleanup(handle);
                return Err(b"error:curl-perform");
            }

            let mut status = 0 as c_long;
            let info_result =
                curl_easy_getinfo(handle, CURLINFO_RESPONSE_CODE, ptr::addr_of_mut!(status));
            curl_easy_cleanup(handle);

            if info_result != CURLE_OK {
                return Err(b"error:curl-getinfo");
            }
            if status != 200 {
                return Err(b"error:http-status");
            }
            if body_bytes == 0 {
                return Err(b"error:empty-body");
            }
        }
        Ok(())
    }

    pub fn write_marker(payload: &[u8]) {
        // SAFETY: the environment/default paths and mode are NUL-terminated,
        // and payload remains valid for the duration of fwrite.
        unsafe {
            let configured = getenv(MARKER_ENV.as_ptr().cast::<c_char>());
            let path = if configured.is_null() || *configured == 0 {
                DEFAULT_MARKER.as_ptr().cast::<c_char>()
            } else {
                configured.cast_const()
            };
            let stream = fopen(path, WRITE_MODE.as_ptr().cast::<c_char>());
            if stream.is_null() {
                return;
            }
            let _ = fwrite(payload.as_ptr().cast::<c_void>(), 1, payload.len(), stream);
            let _ = fclose(stream);
        }
    }
}

#[cfg(target_os = "windows")]
mod platform {
    use core::ffi::{c_char, c_void};
    use core::mem::{self, MaybeUninit};
    use core::ptr;

    type Bool = i32;
    type Dword = u32;
    type Hinternet = *mut c_void;
    type Handle = *mut c_void;

    const WINHTTP_ACCESS_TYPE_DEFAULT_PROXY: Dword = 0;
    const INTERNET_DEFAULT_HTTPS_PORT: u16 = 443;
    const WINHTTP_FLAG_SECURE: Dword = 0x0080_0000;
    const WINHTTP_QUERY_STATUS_CODE: Dword = 19;
    const WINHTTP_QUERY_FLAG_NUMBER: Dword = 0x2000_0000;

    const GENERIC_WRITE: Dword = 0x4000_0000;
    const FILE_SHARE_READ: Dword = 0x0000_0001;
    const FILE_SHARE_WRITE: Dword = 0x0000_0002;
    const CREATE_ALWAYS: Dword = 2;
    const FILE_ATTRIBUTE_NORMAL: Dword = 0x0000_0080;
    const INVALID_HANDLE_VALUE: Handle = usize::MAX as Handle;

    const fn wide<const N: usize>(bytes: &[u8; N]) -> [u16; N] {
        let mut result = [0u16; N];
        let mut index = 0;
        while index < N {
            result[index] = bytes[index] as u16;
            index += 1;
        }
        result
    }

    static USER_AGENT: [u16; 27] = wide(b"reflektor-rust-fixture/1.0\0");
    static HOST: [u16; 12] = wide(b"example.com\0");
    static VERB: [u16; 4] = wide(b"GET\0");
    static PATH: [u16; 2] = wide(b"/\0");
    static MARKER_ENV: &[u8] = b"REFLEKTOR_MARKER\0";
    static DEFAULT_MARKER: &[u8] = b"C:\\Windows\\Temp\\reflektor_rust_marker.txt\0";
    const MARKER_PATH_CAPACITY: usize = 4096;

    #[link(name = "winhttp")]
    unsafe extern "system" {
        fn WinHttpOpen(
            user_agent: *const u16,
            access_type: Dword,
            proxy_name: *const u16,
            proxy_bypass: *const u16,
            flags: Dword,
        ) -> Hinternet;
        fn WinHttpSetTimeouts(
            session: Hinternet,
            resolve_timeout: i32,
            connect_timeout: i32,
            send_timeout: i32,
            receive_timeout: i32,
        ) -> Bool;
        fn WinHttpConnect(
            session: Hinternet,
            server_name: *const u16,
            server_port: u16,
            reserved: Dword,
        ) -> Hinternet;
        fn WinHttpOpenRequest(
            connect: Hinternet,
            verb: *const u16,
            object_name: *const u16,
            version: *const u16,
            referrer: *const u16,
            accept_types: *const *const u16,
            flags: Dword,
        ) -> Hinternet;
        fn WinHttpSendRequest(
            request: Hinternet,
            headers: *const u16,
            headers_length: Dword,
            optional: *mut c_void,
            optional_length: Dword,
            total_length: Dword,
            context: usize,
        ) -> Bool;
        fn WinHttpReceiveResponse(request: Hinternet, reserved: *mut c_void) -> Bool;
        fn WinHttpQueryHeaders(
            request: Hinternet,
            info_level: Dword,
            name: *const u16,
            buffer: *mut c_void,
            buffer_length: *mut Dword,
            index: *mut Dword,
        ) -> Bool;
        fn WinHttpReadData(
            request: Hinternet,
            buffer: *mut c_void,
            bytes_to_read: Dword,
            bytes_read: *mut Dword,
        ) -> Bool;
        fn WinHttpCloseHandle(handle: Hinternet) -> Bool;
    }

    #[link(name = "kernel32")]
    unsafe extern "system" {
        fn GetEnvironmentVariableA(name: *const c_char, buffer: *mut c_char, size: Dword) -> Dword;
        fn CreateFileA(
            path: *const c_char,
            desired_access: Dword,
            share_mode: Dword,
            security_attributes: *mut c_void,
            creation_disposition: Dword,
            flags_and_attributes: Dword,
            template_file: Handle,
        ) -> Handle;
        fn WriteFile(
            file: Handle,
            buffer: *const c_void,
            bytes_to_write: Dword,
            bytes_written: *mut Dword,
            overlapped: *mut c_void,
        ) -> Bool;
        fn CloseHandle(handle: Handle) -> Bool;
    }

    pub fn get_example_com() -> Result<(), &'static [u8]> {
        // SAFETY: all WinHTTP handles are checked before use and closed on every
        // exit path; string pointers refer to static NUL-terminated UTF-16 data.
        unsafe {
            let session = WinHttpOpen(
                USER_AGENT.as_ptr(),
                WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
                ptr::null(),
                ptr::null(),
                0,
            );
            if session.is_null() {
                return Err(b"error:winhttp-open");
            }
            if WinHttpSetTimeouts(session, 10_000, 10_000, 10_000, 20_000) == 0 {
                let _ = WinHttpCloseHandle(session);
                return Err(b"error:winhttp-timeouts");
            }

            let connect = WinHttpConnect(session, HOST.as_ptr(), INTERNET_DEFAULT_HTTPS_PORT, 0);
            if connect.is_null() {
                let _ = WinHttpCloseHandle(session);
                return Err(b"error:winhttp-connect");
            }

            let request = WinHttpOpenRequest(
                connect,
                VERB.as_ptr(),
                PATH.as_ptr(),
                ptr::null(),
                ptr::null(),
                ptr::null(),
                WINHTTP_FLAG_SECURE,
            );
            if request.is_null() {
                let _ = WinHttpCloseHandle(connect);
                let _ = WinHttpCloseHandle(session);
                return Err(b"error:winhttp-request");
            }

            let sent = WinHttpSendRequest(request, ptr::null(), 0, ptr::null_mut(), 0, 0, 0);
            if sent == 0 || WinHttpReceiveResponse(request, ptr::null_mut()) == 0 {
                close_all(request, connect, session);
                return Err(b"error:winhttp-send");
            }

            let mut status = 0u32;
            let mut status_size = mem::size_of::<Dword>() as Dword;
            if WinHttpQueryHeaders(
                request,
                WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
                ptr::null(),
                ptr::addr_of_mut!(status).cast::<c_void>(),
                ptr::addr_of_mut!(status_size),
                ptr::null_mut(),
            ) == 0
            {
                close_all(request, connect, session);
                return Err(b"error:winhttp-status");
            }

            let mut first_byte = 0u8;
            let mut bytes_read = 0u32;
            let read_ok = WinHttpReadData(
                request,
                ptr::addr_of_mut!(first_byte).cast::<c_void>(),
                1,
                ptr::addr_of_mut!(bytes_read),
            );
            close_all(request, connect, session);

            if status != 200 {
                return Err(b"error:http-status");
            }
            if read_ok == 0 || bytes_read == 0 {
                return Err(b"error:empty-body");
            }
        }
        Ok(())
    }

    unsafe fn close_all(request: Hinternet, connect: Hinternet, session: Hinternet) {
        // SAFETY: callers pass live WinHTTP handles exactly once.
        unsafe {
            let _ = WinHttpCloseHandle(request);
            let _ = WinHttpCloseHandle(connect);
            let _ = WinHttpCloseHandle(session);
        }
    }

    pub fn write_marker(payload: &[u8]) {
        let mut configured_path = MaybeUninit::<[u8; MARKER_PATH_CAPACITY]>::uninit();
        let configured_path_ptr = configured_path.as_mut_ptr().cast::<u8>();
        // SAFETY: configured_path is writable for the supplied size, and all
        // kernel32 pointers remain valid for the duration of each call.
        unsafe {
            let configured_len = GetEnvironmentVariableA(
                MARKER_ENV.as_ptr().cast::<c_char>(),
                configured_path_ptr.cast::<c_char>(),
                MARKER_PATH_CAPACITY as Dword,
            );
            let path = if configured_len != 0 && configured_len < MARKER_PATH_CAPACITY as Dword {
                configured_path_ptr.cast_const()
            } else {
                DEFAULT_MARKER.as_ptr()
            };

            let file = CreateFileA(
                path.cast::<c_char>(),
                GENERIC_WRITE,
                FILE_SHARE_READ | FILE_SHARE_WRITE,
                ptr::null_mut(),
                CREATE_ALWAYS,
                FILE_ATTRIBUTE_NORMAL,
                ptr::null_mut(),
            );
            if file == INVALID_HANDLE_VALUE {
                return;
            }
            let mut written = 0u32;
            let _ = WriteFile(
                file,
                payload.as_ptr().cast::<c_void>(),
                payload.len() as Dword,
                ptr::addr_of_mut!(written),
                ptr::null_mut(),
            );
            let _ = CloseHandle(file);
        }
    }
}
