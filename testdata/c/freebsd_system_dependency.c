#include <stdio.h>
#include <stdlib.h>

int archive_version_number(void);

__attribute__((visibility("default"))) void StartW(void) {
    const char *marker_path = getenv("REFLEKTOR_MARKER");
    if (marker_path == NULL || archive_version_number() <= 0) {
        return;
    }

    FILE *marker = fopen(marker_path, "wb");
    if (marker == NULL) {
        return;
    }
    (void)fwrite("ok", 1, 2, marker);
    (void)fclose(marker);
}
