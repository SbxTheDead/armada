/*
 * install_ftp.c — example Armada module: install and enable an FTP server.
 *
 * Detects the device's package manager and runs the right install command, so
 * the single compiled ftp.wasm works across Debian/Ubuntu, Alpine, Fedora/RHEL,
 * and Arch. Build it to ../dist/ftp.wasm, then:  armada run ftp --all
 */
#include "armada.h"

int main(int argc, char **argv) {
	(void)argc;
	(void)argv;

	armada_log("armada: installing FTP server (vsftpd)\n");

	if (armada_have("apt-get")) {
		return armada_exec(
			"apt-get update && apt-get install -y vsftpd && "
			"systemctl enable --now vsftpd");
	}
	if (armada_have("apk")) {
		return armada_exec(
			"apk add vsftpd && rc-update add vsftpd && rc-service vsftpd start");
	}
	if (armada_have("dnf")) {
		return armada_exec(
			"dnf install -y vsftpd && systemctl enable --now vsftpd");
	}
	if (armada_have("pacman")) {
		return armada_exec("pacman -S --noconfirm vsftpd");
	}

	armada_log("armada: no supported package manager found\n");
	return 1;
}
