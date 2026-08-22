#include <stdint.h>

#if defined(_WIN32)
#define BEACON_IMPORT __declspec(dllimport)
#else
#define BEACON_IMPORT
#endif

typedef struct {
    char *original;
    char *buffer;
    int32_t length;
    int32_t size;
} datap;

BEACON_IMPORT void BeaconDataParse(datap *parser, char *buffer, int32_t size);
BEACON_IMPORT int32_t BeaconDataInt(datap *parser);
BEACON_IMPORT int16_t BeaconDataShort(datap *parser);
BEACON_IMPORT char *BeaconDataExtract(datap *parser, int32_t *size);
BEACON_IMPORT void BeaconOutput(int32_t type, char *data, int32_t length);
BEACON_IMPORT void BeaconPrintf(int32_t type, const char *format, ...);

// External linkage is intentional. PIC ELF compilers access this symbol through
// the GOT even though it is defined in the same relocatable object; native
// Mach-O compilers emit their corresponding section-relative relocation pair.
char bof_pic_global[] = "bof-pic-defined-global";

void go(char *buffer, int32_t length) {
    static char success[] = "bof-e2e-ok";
    static char failure[] = "bof-e2e-invalid-arguments";
    datap parser;
    int32_t text_length = 0;
    char *text;

    BeaconDataParse(&parser, buffer, length);
    if (BeaconDataInt(&parser) != 0x12345678) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    if (BeaconDataShort(&parser) != 0x1234) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    text = BeaconDataExtract(&parser, &text_length);
    if (text == (char *)0 || text_length != 4 || text[0] != 'b' || text[1] != 'o' || text[2] != 'f' || text[3] != 0) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    BeaconOutput(0, success, (int32_t)(sizeof(success) - 1));
    // Keep these as literals so ARM64/COFF emits a section-symbol ADRP
    // relocation with a non-zero implicit byte addend.
#if defined(BOF_DARWIN_ARM64_MACHO)
    // Native Apple arm64 variadic arguments use a different ABI. Exercise the
    // native Mach-O relocation path with BeaconOutput; the legacy Darwin ELF
    // fixture retains the BeaconPrintf callback test.
    static char formatted[] = "bof-printf=7:callback-ok";
    BeaconOutput(0, formatted, (int32_t)(sizeof(formatted) - 1));
#else
    BeaconPrintf(0, "bof-printf=%d:%s", 7, "callback-ok");
#endif
    BeaconOutput(0, bof_pic_global, (int32_t)(sizeof(bof_pic_global) - 1));
}
