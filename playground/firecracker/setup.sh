#!/usr/bin/env bash
# setup.sh — download Firecracker binaries and build the minimal Alpine rootfs
# with the guest-signal binary embedded.
#
# Run once before `go run .`.
# Requires: curl, tar, truncate, mkfs.ext4, mount (root or user-namespace),
#           go (for guest-signal build).
#
# Output files (all in ~/.roost/images/):
#   firecracker   – Firecracker v1.11.0 binary
#   vmlinux       – Linux kernel provided by Firecracker project
#   rootfs.ext4   – Alpine 3.20 miniroot + guest-signal + /sbin/init wrapper

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
  echo "   saved to ${FC_BIN}"
else
  echo "✓ firecracker already present"
fi

# ── 2. vmlinux kernel ────────────────────────────────────────────────────
KERNEL="${IMG_DIR}/vmlinux"
if [[ ! -f "${KERNEL}" ]]; then
  echo "→ downloading vmlinux (Firecracker hello kernel)…"
  curl -fsSL -o "${KERNEL}" \
    "https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/${ARCH}/kernels/vmlinux.bin"
  echo "   saved to ${KERNEL}"
else
  echo "✓ vmlinux already present"
fi

# ── 3. guest-signal binary ───────────────────────────────────────────────
GUEST_SIGNAL="${SCRIPT_DIR}/guest-signal"
echo "→ building guest-signal binary…"
(cd "${SCRIPT_DIR}/guest" && \
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "${GUEST_SIGNAL}" .)
echo "   built at ${GUEST_SIGNAL} ($(du -sh "${GUEST_SIGNAL}" | cut -f1))"

# ── 4. Alpine rootfs ext4 image ──────────────────────────────────────────
ROOTFS="${IMG_DIR}/rootfs.ext4"
if [[ ! -f "${ROOTFS}" ]]; then
  echo "→ building Alpine rootfs…"

  ALPINE_VERSION="3.20"
  ALPINE_ARCH="x86_64"
  ALPINE_MIRROR="https://dl-cdn.alpinelinux.org/alpine"
  ALPINE_URL="${ALPINE_MIRROR}/v${ALPINE_VERSION}/releases/${ALPINE_ARCH}/alpine-minirootfs-${ALPINE_VERSION}.0-${ALPINE_ARCH}.tar.gz"
  ALPINE_TGZ="$(mktemp -t alpine.XXXXXX.tgz)"
  curl -fsSL -o "${ALPINE_TGZ}" "${ALPINE_URL}"

  # Create a 200 MiB ext4 image.
  truncate -s 200M "${ROOTFS}"
  mkfs.ext4 -F -L rootfs "${ROOTFS}"

  # Mount and populate (requires unshare -r for rootless, or run as root).
  MNT="$(mktemp -d -t fc-rootfs.XXXXXX)"

  # Prefer rootless mount via fuse2fs if available.
  if command -v fuse2fs &>/dev/null; then
    fuse2fs "${ROOTFS}" "${MNT}" -o fakeroot,rw
    UNMOUNT="fusermount -u ${MNT}"
  else
    # Fallback: requires root / sudo.
    sudo mount -o loop "${ROOTFS}" "${MNT}"
    UNMOUNT="sudo umount ${MNT}"
  fi

  tar -xzf "${ALPINE_TGZ}" -C "${MNT}"
  rm "${ALPINE_TGZ}"

  # Copy guest-signal binary.
  cp "${GUEST_SIGNAL}" "${MNT}/usr/local/bin/guest-signal"

  # Write a minimal /sbin/init that:
  #  1. Mounts essential pseudo-filesystems.
  #  2. Runs guest-signal in the background.
  #  3. Drops to a shell for manual testing.
  cat > "${MNT}/sbin/init" <<'INIT'
#!/bin/sh
mount -t proc none /proc
mount -t sysfs none /sys
mount -t devtmpfs none /dev 2>/dev/null || true

# Signal host that the VM is ready.
/usr/local/bin/guest-signal &

exec /bin/sh
INIT
  chmod +x "${MNT}/sbin/init"

  eval "${UNMOUNT}"
  rmdir "${MNT}"
  echo "   saved to ${ROOTFS} ($(du -sh "${ROOTFS}" | cut -f1))"
else
  echo "✓ rootfs already present"
fi

echo ""
echo "All prerequisites ready. Run the PoC:"
echo "  cd ${SCRIPT_DIR} && go run . --runs 5 --fleet 5"
