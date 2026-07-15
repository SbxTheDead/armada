/*
 * ftp.c — native Armada module: install and enable an FTP server.
 *
 * This is the NATIVE version (compiled per-arch by build-native.sh). Unlike the
 * WASM version (install_ftp.c), it runs as a normal native binary with full
 * access, so it uses the device shell directly via system(). Its stdout is
 * captured and its exit code is the task result.
 *
 *   cd modules && ./build-native.sh ftp src/ftp.c
 *   armada run ftp --all
 */
#include <stdio.h>
#include <stdlib.h>

/* returns 1 if cmd exists on the device, else 0 */
static int have(const char *cmd) {
	char buf[256];
	snprintf(buf, sizeof buf, "command -v %s >/dev/null 2>&1", cmd);
	return system(buf) == 0;
}

/* run a shell command, return 0 on success else 1 */
static int run(const char *cmd) {
	return system(cmd) == 0 ? 0 : 1;
}

int main(void) {
	printf("armada: installing FTP server (vsftpd)\n");
	fflush(stdout);

	if (have("apt-get"))
		return run("apt-get update && apt-get install -y vsftpd && systemctl enable --now vsftpd");
	if (have("apk"))
		return run("apk add vsftpd && rc-update add vsftpd && rc-service vsftpd start");
	if (have("dnf"))
		return run("dnf install -y vsftpd && systemctl enable --now vsftpd");
	if (have("pacman"))
		return run("pacman -S --noconfirm vsftpd");

	printf("armada: no supported package manager found\n");
	return 1;
}
