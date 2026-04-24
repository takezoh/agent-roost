#!/usr/bin/env bash
# setup.sh — download Firecracker binaries and build a minimal rootfs
# that uses our own Go binary as PID 1 (init).
#
# Run once before `go run .`.
# Requires: curl, truncate, mkfs.ext4, sudo (for mount).
#
# Output files (all in ~/.roost/images/):
#   firecracker   – Firecracker v1.11.0 binary
#   vmlinux       – Linux kernel from Firecracker's quick-start guide
#   rootfs.ext4   – minimal ext4 with guest-signal as /sbin/init

set -euo pipefail

FC_VERSION="v1.11.0"
ARCH="x86_64"
IMG_DIR="${HOME}/.roost/images"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

mkdir -p "${IMG_DIR}"

# ── 1. Firecracker binary ─────────────────────────────────────────────────
FC_BIN="${IMG_DIR}/firecracker"
if [[ ! -f "${FC_BIN}" ]]; then
  echo "→ downloading Firecracker ${FC_VERSION}…"
  TMP_TGZ="$(mktemp -t fc.XXXXXX.tgz)"
  curl -fsSL -o "${TMP_TGZ}" \
    "https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-${ARCH}.tgz"
  tar -xzf "${TMP_TGZ}" --strip-components=1 -C "${IMG_DIR}" \
    "release-${FC_VERSION}-${ARCH}/firecracker-${FC_VERSION}-${ARCH}"
  mv "${IMG_DIR}/firecracker-${FC_VERSION}-${ARCH}" "${FC_BIN}"
  chmod +x "${FC_BIN}"
  rm "${TMP_TGZ}"
  echo "   saved: ${FC_BIN}"
else
  echo "✓ firecracker already present"
fi

# ── 2. vmlinux kernel ────────────────────────────────────────────────────
KERNEL="${IMG_DIR}/vmlinux"
if [[ ! -f "${KERNEL}" ]]; then
  echo "→ downloading vmlinux (Firecracker quick-start kernel)…"
  curl -fsSL -o "${KERNEL}" \
    "https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/${ARCH}/kernels/vmlinux.bin"
  echo "   saved: ${KERNEL}"
else
  echo "✓ vmlinux already present"
fi

# ── 3. guest-signal binary (our custom PID 1 / init) ─────────────────────
GUEST_BIN="${SCRIPT_DIR}/guest-signal"
echo "→ building guest-signal (linux/amd64, static)…"
(cd "${SCRIPT_DIR}/guest" && \
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags="-s -w" -o "${GUEST_BIN}" .)
echo "   built: ${GUEST_BIN}  ($(du -sh "${GUEST_BIN}" | cut -f1))"

# ── 4. Minimal ext4 rootfs ───────────────────────────────────────────────
# We don't use a distro rootfs.  The only binary in the image is our
# statically-linked guest-signal, which acts as PID 1 (init).
ROOTFS="${IMG_DIR}/rootfs.ext4"
if [[ -f "${ROOTFS}" ]]; then
  echo "→ removing stale rootfs to rebuild from scratch…"
  rm "${ROOTFS}"
fi

echo "→ creating minimal ext4 rootfs (64 MiB)…"
truncate -s 64M "${ROOTFS}"
mkfs.ext4 -F -L rootfs -q "${ROOTFS}"

MNT="$(mktemp -d -t fc-rootfs.XXXXXX)"
sudo mount -o loop "${ROOTFS}" "${MNT}"

# Create the bare minimum directory structure.
# devtmpfs needs /dev; proc needs /proc; sysfs needs /sys.
# The kernel looks for init at /sbin/init first.
sudo mkdir -p "${MNT}"/{dev,proc,sys,tmp,sbin}

# Install our Go binary as /sbin/init.
sudo cp "${GUEST_BIN}" "${MNT}/sbin/init"
sudo chmod +x "${MNT}/sbin/init"

sudo umount "${MNT}"
rmdir "${MNT}"

echo "   saved: ${ROOTFS}  ($(du -sh "${ROOTFS}" | cut -f1))"
echo ""
echo "All prerequisites ready. Run the PoC:"
echo "  cd ${SCRIPT_DIR} && go run . --runs 5 --fleet 5"
