#include <stdint.h>

#if defined(_WIN32)
#define BEACON_IMPORT __declspec(dllimport)
#else
#define BEACON_IMPORT
#endif

typedef uint16_t bof_wchar_t;

typedef struct {
    char *original;
    char *buffer;
    int32_t length;
    int32_t size;
} datap;

BEACON_IMPORT void BeaconDataParse(datap *parser, char *buffer, int32_t size);
BEACON_IMPORT char *BeaconDataExtractOrNull(datap *parser, int32_t *size);
BEACON_IMPORT void BeaconOutput(int32_t type, char *data, int32_t length);
BEACON_IMPORT int32_t toWideChar(char *source, bof_wchar_t *destination, int32_t maximum);

// These imports are intentionally supplied by BOFLoadOptions.ResolveSymbol.
BEACON_IMPORT uintptr_t HostResolvedValue(void);
BEACON_IMPORT void BeaconInjectProcess(void);
#if defined(BOF_DARWIN)
extern BEACON_IMPORT uintptr_t HostResolvedData;
#endif

char bof_options_global[] = "not-an-entry";

void custom_entry(char *buffer, int32_t length) {
    static char success[] = "bof-options-ok";
    static char failure[] = "bof-options-invalid";
    static char source[] = "ok";
    datap parser;
    int32_t text_length = 0;
    char *empty;
    char *text;
    bof_wchar_t wide[3] = {0, 0, 0};

    // Preserve a relocation for a host-integrated privileged callback without
    // invoking it during the test.
    if (length < 0) {
        BeaconInjectProcess();
    }

    BeaconDataParse(&parser, buffer, length);
    empty = BeaconDataExtractOrNull(&parser, &text_length);
    if (empty != (char *)0 || text_length != 1) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    text = BeaconDataExtractOrNull(&parser, &text_length);
    if (text == (char *)0 || text_length != 8 || text[0] != 'o' || text[7] != 0) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    if (!toWideChar(source, wide, (int32_t)sizeof(wide)) || wide[0] != 'o' || wide[1] != 'k' || wide[2] != 0) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    if (HostResolvedValue() != (uintptr_t)0x42) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
#if defined(BOF_DARWIN)
    if (HostResolvedData != (uintptr_t)0x43) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
#endif
    BeaconOutput(0, success, (int32_t)(sizeof(success) - 1));
}
