#!/usr/bin/env bash
# 3x-ui 机群统一升级脚本（本 fork 专用）
#   面板：升级到本次 fork 构建（缺省取 dev-latest 资产）
#   Xray：固定 26.7.28 —— mihomo 客户端 REALITY 兼容的最后一个版本
# 设计要点：幂等、带时间戳备份、失败自动回滚、每步验证、日志脱敏
# 用法：sudo bash upgrade-fleet.sh [--dry-run]
set -Eeuo pipefail

XUI_TAG="${XUI_TAG:-dev-latest}"
XRAY_VER="${XRAY_VER:-v26.7.28}"
REPO="${REPO:-Lidociane/3x-ui}"
TS="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="/root/backup"
LOG_DIR="/root/upgrade-logs"
LOG="$LOG_DIR/upgrade-$TS.log"
WORK="$(mktemp -d /tmp/xui-upgrade.XXXXXX)"
DRY_RUN=0
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=1

mkdir -p "$BACKUP_DIR" "$LOG_DIR"
exec > >(tee -a "$LOG") 2>&1
trap 'rm -rf "$WORK"' EXIT

log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# ---------- 1. 前置检查 ----------
[[ $EUID -eq 0 ]] || fail "需要 root"
[[ -x /usr/local/x-ui/x-ui ]] || fail "/usr/local/x-ui/x-ui 不存在（这台没装 3x-ui？）"
command -v python3 >/dev/null || fail "缺少 python3（需要读 DB 校验）"

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ; XRAY_ASSET="Xray-linux-64.zip" ;;
  aarch64|arm64)  ARCH=arm64 ; XRAY_ASSET="Xray-linux-arm64-v8a.zip" ;;
  armv7l)         ARCH=armv7 ; XRAY_ASSET="Xray-linux-arm32-v7a.zip" ;;
  *) fail "不支持的架构：$(uname -m)" ;;
esac

case "$ARCH" in
  amd64) XRAY_BIN="xray-linux-amd64" ;;
  arm64) XRAY_BIN="xray-linux-arm64" ;;
  armv7) XRAY_BIN="xray-linux-arm32" ;;
esac

XUI_URL="https://github.com/$REPO/releases/download/$XUI_TAG/x-ui-linux-$ARCH.tar.gz"
XRAY_URL="https://github.com/XTLS/Xray-core/releases/download/$XRAY_VER/$XRAY_ASSET"

log "主机=$(hostname) 架构=$ARCH"
log "面板资产=$XUI_URL"
log "Xray    =$XRAY_URL"

OLD_XUI="$(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1 || echo unknown)"
OLD_XRAY="$(/usr/local/x-ui/bin/$XRAY_BIN -version 2>/dev/null | head -1 || echo unknown)"
log "升级前：x-ui=$OLD_XUI | xray=$OLD_XRAY"

if [[ $DRY_RUN -eq 1 ]]; then
  log "DRY-RUN：不下载、不写盘、不重启"
  exit 0
fi

# ---------- 2. 备份（二进制 + 数据库，带时间戳） ----------
tar czf "$BACKUP_DIR/xui-dir-before-upgrade-$TS.tar.gz" -C /usr/local x-ui 2>/dev/null || fail "备份 /usr/local/x-ui 失败"
if [[ -f /etc/x-ui/x-ui.db ]]; then
  python3 - "$BACKUP_DIR/x-ui-db-before-upgrade-$TS.db" <<'PY' || fail "备份数据库失败"
import sqlite3, sys
src = sqlite3.connect('/etc/x-ui/x-ui.db')
dst = sqlite3.connect(sys.argv[1])
src.backup(dst); dst.close(); src.close()
PY
fi
log "备份完成：$BACKUP_DIR/*before-upgrade-$TS*"

rollback() {
  log "回滚：恢复备份 $BACKUP_DIR/xui-dir-before-upgrade-$TS.tar.gz"
  systemctl stop x-ui 2>/dev/null || true
  rm -rf /usr/local/x-ui
  tar xzf "$BACKUP_DIR/xui-dir-before-upgrade-$TS.tar.gz" -C /usr/local || log "回滚解包失败，请人工介入"
  systemctl start x-ui 2>/dev/null || true
  log "回滚完成，当前 x-ui=$(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1)"
}

# ---------- 3. 下载并校验 ----------
log "下载面板包"
if [[ -n "${XUI_LOCAL:-}" ]]; then
  cp -f "$XUI_LOCAL" "$WORK/xui.tar.gz" || fail "复制本地面板包失败：$XUI_LOCAL"
else
  curl -fL --retry 3 --retry-delay 3 --connect-timeout 15 -o "$WORK/xui.tar.gz" "$XUI_URL" \
    || fail "下载面板包失败：$XUI_URL"
fi
mkdir -p "$WORK/xui" && tar xzf "$WORK/xui.tar.gz" -C "$WORK/xui" || fail "面板包解压失败"
[[ -f "$WORK/xui/x-ui/x-ui" ]] || fail "面板包结构异常"

log "下载 Xray $XRAY_VER"
if [[ -n "${XRAY_LOCAL:-}" ]]; then
  cp -f "$XRAY_LOCAL" "$WORK/xray.zip" || fail "复制本地 Xray 包失败：$XRAY_LOCAL"
else
  curl -fL --retry 3 --retry-delay 3 --connect-timeout 15 -o "$WORK/xray.zip" "$XRAY_URL" \
    || fail "下载 Xray 失败：$XRAY_URL"
fi
if [[ "$XRAY_ASSET" == *.zip ]]; then
  # 不依赖 unzip（最小云镜像常常没有）：python3 从这里起就是硬依赖
  python3 -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' \
    "$WORK/xray.zip" "$WORK" || fail "Xray 包解压失败"
fi
[[ -f "$WORK/xray" ]] || fail "Xray 包结构异常"

# ---------- 4. 安装 ----------
systemctl stop x-ui || log "停止服务返回非 0（可能本来已停）"

install -m 755 "$WORK/xui/x-ui/x-ui" /usr/local/x-ui/x-ui || { rollback; fail "安装面板二进制失败"; }
install -m 755 "$WORK/xray" "/usr/local/x-ui/bin/$XRAY_BIN" || { rollback; fail "安装 Xray 失败"; }
for f in geosite.dat geoip.dat; do
  [[ -f "$WORK/xui/x-ui/bin/$f" ]] && cp -f "$WORK/xui/x-ui/bin/$f" "/usr/local/x-ui/bin/$f"
done

systemctl start x-ui || { rollback; fail "启动失败"; }

# ---------- 5. 验证（版本 / 面板 / 入站端口） ----------
sleep 8
NEW_XUI="$(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1 || echo unknown)"
NEW_XRAY="$(/usr/local/x-ui/bin/$XRAY_BIN -version 2>/dev/null | head -1 || echo unknown)"
log "升级后：x-ui=$NEW_XUI | xray=$NEW_XRAY"
[[ "$NEW_XRAY" == *"26.7.28"* ]] || { rollback; fail "Xray 版本不是 26.7.28（mihomo 兼容版）"; }
systemctl is-active --quiet x-ui || { rollback; fail "x-ui 未处于 active"; }

PORT_INFO="$(python3 - <<'PY'
import sqlite3
try:
    c = sqlite3.connect('file:/etc/x-ui/x-ui.db?mode=ro', uri=True)
    port = c.execute("select value from settings where key='webPort'").fetchone()
    port = port[0] if port else '?'
    n = c.execute('select count(*) from inbounds where enable=1').fetchone()[0]
    print(f'{port} {n}')
except Exception:
    print('? ?')
PY
)"
WEB_PORT="${PORT_INFO%% *}"
log "面板端口=$WEB_PORT 启用入站数=${PORT_INFO##* }"
[[ "$WEB_PORT" != "?" ]] && { curl -sk -o /dev/null -m 10 -w '' "https://127.0.0.1:$WEB_PORT/" || log "面板端口探测无响应（继续，人工确认）"; }

log "成功：x-ui=$NEW_XUI | xray=$NEW_XRAY"
log "备份保留在 $BACKUP_DIR（回滚用 xui-dir-before-upgrade-$TS.tar.gz）"
