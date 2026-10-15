#include <dlfcn.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/lwp.h>
#include <sys/mman.h>
#include <unistd.h>

static void *identify_thread(void *data) {
	*(lwpid_t *)data = lwp_gettid();
	return NULL;
}

int main(void) {
	long page_size = sysconf(_SC_PAGESIZE);
	if (page_size <= 0) {
		perror("sysconf(_SC_PAGESIZE)");
		return 1;
	}

	void *code = mmap(NULL, (size_t)page_size, PROT_READ | PROT_WRITE,
		MAP_PRIVATE | MAP_ANON, -1, 0);
	if (code == MAP_FAILED) {
		perror("mmap");
		return 1;
	}
	const unsigned char return_42[] = {0xb8, 42, 0, 0, 0, 0xc3};
	memcpy(code, return_42, sizeof(return_42));
	if (mprotect(code, (size_t)page_size, PROT_READ | PROT_EXEC) != 0) {
		perror("mprotect(PROT_READ|PROT_EXEC)");
		return 1;
	}
	int (*execute)(void) = (int (*)(void))code;
	int executed = execute();
	if (munmap(code, (size_t)page_size) != 0 || executed != 42) {
		fprintf(stderr, "executable mapping returned %d or failed to unmap\n", executed);
		return 1;
	}

	void *library = dlopen("./libprobe.so", RTLD_NOW | RTLD_LOCAL);
	if (library == NULL) {
		fprintf(stderr, "dlopen: %s\n", dlerror());
		return 1;
	}
	int (*call_export)(int) = (int (*)(int))dlsym(library, "reflektor_dragonfly_probe");
	if (call_export == NULL) {
		fprintf(stderr, "dlsym: %s\n", dlerror());
		return 1;
	}
	int exported = call_export(42);
	if (dlclose(library) != 0 || exported != 77) {
		fprintf(stderr, "shared-library export returned %d or failed to close\n", exported);
		return 1;
	}

	lwpid_t entry_thread = lwp_gettid();
	lwpid_t worker_thread = 0;
	pthread_t worker;
	int thread_error = pthread_create(&worker, NULL, identify_thread, &worker_thread);
	if (thread_error != 0) {
		fprintf(stderr, "pthread_create: %s\n", strerror(thread_error));
		return 1;
	}
	thread_error = pthread_join(worker, NULL);
	if (thread_error != 0 || worker_thread == 0 || worker_thread == entry_thread) {
		fprintf(stderr, "pthread_join: %s; thread IDs %d and %d\n",
			strerror(thread_error), (int)entry_thread, (int)worker_thread);
		return 1;
	}

	printf("dragonfly probe: executable memory=%d shared export=%d entry thread=%d worker thread=%d\n",
		executed, exported, (int)entry_thread, (int)worker_thread);
	return 0;
}
