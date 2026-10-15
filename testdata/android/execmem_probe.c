/* Android/arm64 shell-domain executable-memory feasibility probe. */
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

int main(void)
{
	void *libc = dlopen("libc.so", RTLD_NOW);
	if (libc == NULL || dlsym(libc, "getpid") == NULL) {
		fprintf(stderr, "Bionic dlopen/dlsym failed: %s\n", dlerror());
		return 1;
	}
	if (dlclose(libc) != 0) {
		fprintf(stderr, "Bionic dlclose failed: %s\n", dlerror());
		return 1;
	}

	long page_size = sysconf(_SC_PAGESIZE);
	if (page_size <= 0) {
		fprintf(stderr, "invalid page size\n");
		return 1;
	}
	void *page = mmap(NULL, (size_t)page_size, PROT_READ | PROT_WRITE,
	    MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
	if (page == MAP_FAILED) {
		perror("mmap RW");
		return 1;
	}
	/* mov w0, #42; ret */
	static const unsigned char code[] = {
	    0x40, 0x05, 0x80, 0x52, 0xc0, 0x03, 0x5f, 0xd6,
	};
	memcpy(page, code, sizeof(code));
	__builtin___clear_cache((char *)page, (char *)page + sizeof(code));
	if (mprotect(page, (size_t)page_size, PROT_READ | PROT_EXEC) != 0) {
		perror("mprotect RW to RX");
		return 1;
	}
	int (*fn)(void) = page;
	if (fn() != 42) {
		fprintf(stderr, "mapped code returned wrong value\n");
		return 1;
	}
	if (munmap(page, (size_t)page_size) != 0) {
		perror("munmap");
		return 1;
	}
	puts("android/arm64: Bionic dlsym and RW->RX mapped code passed");
	return 0;
}
