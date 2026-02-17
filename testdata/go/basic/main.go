package main

/*
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#if defined(_WIN32)
#define REFLEKTOR_DEFAULT_MARKER "C:\\Windows\\Temp\\reflektor_marker.txt"
#define REFLEKTOR_EXPORT __declspec(dllexport)
#else
#define REFLEKTOR_DEFAULT_MARKER "/tmp/reflektor_marker.txt"
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#endif

REFLEKTOR_EXPORT void StartW(void) {
	const char *marker = getenv("REFLEKTOR_MARKER");
	if (marker == NULL || marker[0] == '\0') {
		marker = REFLEKTOR_DEFAULT_MARKER;
	}

	FILE *f = fopen(marker, "wb");
	if (f == NULL) {
		return;
	}

	const unsigned char content[2] = {'o', 'k'};
	(void)fwrite(content, 1, sizeof(content), f);
	(void)fclose(f);
}

REFLEKTOR_EXPORT int StartWStatus(void) {
	StartW();
	return 1337;
}
*/
import "C"

func main() {}
