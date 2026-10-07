#!/bin/sh
find_base(){
HDD_MOUNT=`/sbin/getcfg Public path -f /etc/config/smb.conf`
local log_tool="/sbin/log_tool -t2 -uSystem -p127.0.0.1 -mlocalhost -a"

if [ -e "$HDD_MOUNT" ]; then
if [ -z "$QINSTALL_PATH" ]; then
BASE_GROUP="/share/HDA_DATA /share/HDB_DATA /share/HDC_DATA /share/HDD_DATA /share/HDE_DATA /share/HDF_DATA /share/HDG_DATA /share/HDH_DATA /share/HDI_DATA /share/HDJ_DATA /share/HDK_DATA /share/HDL_DATA /share/MD0_DATA /share/MD1_DATA /share/MD2_DATA /share/MD3_DATA"
publicdir=`/sbin/getcfg Public path -f /etc/config/smb.conf`
if [ ! -z $publicdir ] && [ -d $publicdir ];then
publicdirp1=`/bin/echo $publicdir | /bin/cut -d "/" -f 2`
publicdirp2=`/bin/echo $publicdir | /bin/cut -d "/" -f 3`
publicdirp3=`/bin/echo $publicdir | /bin/cut -d "/" -f 4`
if [ ! -z $publicdirp1 ] && [ ! -z $publicdirp2 ] && [ ! -z $publicdirp3 ]; then
[ -d "/${publicdirp1}/${publicdirp2}/Public" ] && QPKG_BASE="/${publicdirp1}/${publicdirp2}"
fi
fi

# Determine BASE installation location by checking where the Public folder is.
if [ -z $QPKG_BASE ]; then
for datadirtest in $BASE_GROUP; do
[ -d $datadirtest/Public ] && QPKG_BASE="/${publicdirp1}/${publicdirp2}"
done
fi
if [ -z $QPKG_BASE ] ; then
echo "The Public share not found."
return 1
fi
QPKG_INSTALL_PATH="${QPKG_BASE}/.qpkg"
QPKG_DIR="${QPKG_INSTALL_PATH}/${QPKG_NAME}"
else
if [ -e "$QINSTALL_PATH" ]; then
QPKG_INSTALL_PATH="${QINSTALL_PATH}"
QPKG_DIR="${QINSTALL_PATH}/${QPKG_NAME}"
else
if [ -x "/usr/local/sbin/notify" ]; then
/usr/local/sbin/notify send -A A039 -C C001 -M 50 -l error -t 3 "[{0}] {1} install failed due to QTS application install volume not found." "App Center" "@DISPLAY@"
echo -1 > /tmp/update_process && exit 1
else
/sbin/log_tool -t2 -uSystem -p127.0.0.1 -mlocalhost -a "[App Center] Failed to install @DISPLAY@. The selected installation volume is missing."
fi
echo -1 > /tmp/update_process && exit 1
fi
fi
return 0
else
if [ 0 = 1 ] && [ -d "/mnt/HDA_ROOT/update_pkg" ]; then
QPKG_INSTALL_PATH="/mnt/HDA_ROOT/update_pkg"
QPKG_DIR="${QPKG_INSTALL_PATH}/@NAME@"
_EXTRACT_DIR="/tmp/@NAME@"
else
if [ -x "/usr/local/sbin/notify" ]; then
/usr/local/sbin/notify send -A A039 -C C001 -M 50 -l error -t 3 "[{0}] {1} install failed due to QTS application install volume not found." "App Center" "@DISPLAY@"
echo -1 > /tmp/update_process && exit 1
else
/sbin/log_tool -t2 -uSystem -p127.0.0.1 -mlocalhost -a "[App Center] Failed to install @DISPLAY@. The selected installation volume is missing."
fi
echo -1 > /tmp/update_process && exit 1
fi
fi
}
wrong_arch(){
if [ -x "/usr/local/sbin/notify" ]; then
/usr/local/sbin/notify send -A A039 -C C001 -M 51 -l error -t 3 "[{0}] {1} {2} install failed due to the platform is incompatible. Please use correct package for installation." "App Center" "@DISPLAY@" "@VER@"
echo -1 > /tmp/update_process && exit 1
else
local wrong_arch_msg="[App Center] Failed to install @DISPLAY@ @VER@. Installation package is incompatible. Use the correct package."
echo "Installation Abort." && echo "$wrong_arch_msg"
/sbin/log_tool -t2 -uSystem -p127.0.0.1 -mlocalhost -a "[App Center] Failed to install @DISPLAY@ @VER@. Installation package is incompatible. Use the correct package."
echo -1 > /tmp/update_process && exit 1
fi
}
arch_ok(){
local cpu_arch=$(/bin/uname -m)
local reject_platform=$(/sbin/getcfg "" Platform -f /etc/platform.conf)
if [ $(/usr/bin/expr match "$reject_platform" "") == 0 ]; then
[ $(/usr/bin/expr match "$cpu_arch" "aarch64") -ne 0 ] || return 1
else
return 1
fi
}
/bin/echo "Install QNAP package on TS-NAS..."
/bin/grep "/mnt/HDA_ROOT" /proc/mounts >/dev/null 2>&1 || exit 1
arch_ok || wrong_arch
find_base
_EXTRACT_DIR="$QPKG_INSTALL_PATH/.tmp-@NAME@"
/bin/mkdir -p $_EXTRACT_DIR || exit 1
script_len=@SCRIPT_LEN@
/bin/dd if="${0}" bs=$script_len skip=1 | /bin/tar -xO | /bin/tar -xzv -C $_EXTRACT_DIR || exit 1
offset=$(/usr/bin/expr $script_len + 20480)
/bin/dd if="${0}" bs=$offset skip=1 | /bin/cat | /bin/dd bs=1024 count=@COUNT@ of=$_EXTRACT_DIR/data.tar.gz || exit 1
[ -f /usr/local/bin/python ] && /usr/local/bin/python -c "with open('$_EXTRACT_DIR/data.tar.gz', 'rw+') as f: f.seek(@DATALEN@); f.truncate()"
offset=$(/usr/bin/expr $offset + @DATALEN@)
( cd $_EXTRACT_DIR && /bin/sh qinstall.sh || echo "Installation Abort." )
/bin/rm -fr $_EXTRACT_DIR && exit 10
exit 1
