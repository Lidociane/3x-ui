#!/usr/bin/env bash
# 3x-ui 机群统一升级脚本（本 fork 专用）
#   面板：升级到本次 fork 构建（缺省取 dev-latest 资产，可用 XUI_LOCAL 走本地包）
#   Xray：固定 26.7.28 —— mihomo 客户端 REALITY 兼容的最后一个版本（mihomo#3476）
# 设计要点：幂等、带时间戳备份（含数据库，回滚时一起还原）、失败自动回滚、
#           校验下载摘要、验证运行中的 xray 而不只是磁盘二进制、日志脱敏
# 用法：sudo bash upgrade-fleet.sh [--dry-run]
#   XUI_TAG / XRAY_VER / XUI_LOCAL / XRAY_LOCAL / KEEP_BACKUPS 可用环境变量覆盖
set -Eeuo pipefail
umask 077   # 备份里含 DB（用户口令 hash、节点 token）与私钥，不能让同机其他用户读到

XUI_TAG="${XUI_TAG:-dev-latest}"
XRAY_VER="${XRAY_VER:-v26.7.28}"
XRAY_PIN="${XRAY_VER#v}"
REPO="${REPO:-Lidociane/3x-ui}"
KEEP_BACKUPS="${KEEP_BACKUPS:-5}"
TS="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="/root/backup"
LOG_DIR="/root/upgrade-logs"
LOG="$LOG_DIR/upgrade-$TS.log"
WORK="$(mktemp -d /tmp/xui-upgrade.XXXXXX)"
DRY_RUN=0
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=1

mkdir -p "$BACKUP_DIR" "$LOG_DIR"
mkdir -p "$WORK"
exec > >(tee -a "$LOG") 2>&1
trap 'rm -rf "$WORK"' EXIT

log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# ---------- 1. 前置检查 ----------
[[ $EUID -eq 0 ]] || fail "需要 root"
[[ -x /usr/local/x-ui/x-ui ]] || fail "/usr/local/x-ui/x-ui 不存在（这台没装 3x-ui？）"
command -v python3 >/dev/null || fail "缺少 python3（读 DB 与解压都要用）"

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ; XRAY_ASSET="Xray-linux-64.zip" ;          XRAY_BIN="xray-linux-amd64" ;;
  aarch64|arm64)  ARCH=arm64 ; XRAY_ASSET="Xray-linux-arm64-v8a.zip" ;   XRAY_BIN="xray-linux-arm64" ;;
  armv7l|armv8l)  ARCH=armv7 ; XRAY_ASSET="Xray-linux-arm32-v7a.zip" ;   XRAY_BIN="xray-linux-arm32" ;;
  *) fail "不支持的架构：$(uname -m)" ;;
esac

XUI_URL="https://github.com/$REPO/releases/download/$XUI_TAG/x-ui-linux-$ARCH.tar.gz"
XUI_SHA_URL="$XUI_URL.sha256"
XRAY_URL="https://github.com/XTLS/Xray-core/releases/download/$XRAY_VER/$XRAY_ASSET"

log "主机=$(hostname) 架构=$ARCH"
log "面板资产=$XUI_URL"
log "Xray    =$XRAY_URL"
OLD_XUI="$(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1 || echo unknown)"
OLD_XRAY="$(/usr/local/x-ui/bin/$XRAY_BIN -version 2>/dev/null | head -1 || echo unknown)"
log "升级前：x-ui=$OLD_XUI | xray=$OLD_XRAY"

# 入站快照：失败时用它证明生产入站没丢
INB_BEFORE="$(python3 - <<'PY' 2>/dev/null || echo "?"
import sqlite3
c = sqlite3.connect('file:/etc/x-ui/x-ui.db?mode=ro', uri=True)
print(','.join(f'{t}:{p}:{port}' for t, p, port in
      c.execute('select tag,protocol,port from inbounds where enable=1 order by id')))
PY
)"
log "入站快照=$INB_BEFORE"

if [[ $DRY_RUN -eq 1 ]]; then
  log "DRY-RUN：不下载、不写盘、不重启"
  exit 0
fi

# ---------- 2. 备份（二进制 + 数据库） ----------
DB_BACKUP="$BACKUP_DIR/x-ui-db-before-upgrade-$TS.db"
if [[ -f /etc/x-ui/x-ui.db ]]; then
  python3 -c 'import sqlite3,sys; s=sqlite3.connect("/etc/x-ui/x-ui.db"); d=sqlite3.connect(sys.argv[1]); s.backup(d); d.close(); s.close()' \
    "$DB_BACKUP" || fail "备份数据库失败（拒绝在无 DB 备份的情况下继续）"
  log "DB 备份：$DB_BACKUP"
elif [[ "${ALLOW_NO_DB_BACKUP:-0}" == "1" ]]; then
  log "警告：/etc/x-ui/x-ui.db 不存在，按 ALLOW_NO_DB_BACKUP=1 继续（Postgres 部署请自行备份）"
else
  fail "/etc/x-ui/x-ui.db 不存在；确认是 Postgres 部署请设 ALLOW_NO_DB_BACKUP=1"
fi

tar czf "$BACKUP_DIR/xui-dir-before-upgrade-$TS.tar.gz" -C /usr/local x-ui || fail "备份 /usr/local/x-ui 失败"
log "目录备份完成"

# 只保留最近 KEEP_BACKUPS 份（tar + db 成对），避免 100MB+ 备份堆满盘
prune_backups() {
  local keep="$1"
  ls -1t "$BACKUP_DIR"/xui-dir-before-upgrade-*.tar.gz 2>/dev/null | tail -n "+$((keep + 1))" | while read -r old; do
    local stamp="${old##*before-upgrade-}"; stamp="${stamp%.tar.gz}"
    rm -f "$old" "$BACKUP_DIR/x-ui-db-before-upgrade-$stamp.db"
    log "清理旧备份 $stamp"
  done
}
prune_backups "$KEEP_BACKUPS"

ROLLED_BACK=0
rollback() {
  [[ $ROLLED_BACK -eq 1 ]] && return 0
  ROLLED_BACK=1
  log "回滚：恢复旧二进制与数据库"
  systemctl stop x-ui 2>/dev/null || true
  rm -rf /usr/local/x-ui
  tar xzf "$BACKUP_DIR/xui-dir-before-upgrade-$TS.tar.gz" -C /usr/local || log "回滚解包失败，请人工介入"
  if [[ -f "$DB_BACKUP" ]]; then
    install -m 600 "$DB_BACKUP" /etc/x-ui/x-ui.db || log "回滚恢复 DB 失败，请人工介入"
  fi
  systemctl start x-ui 2>/dev/null || true
  if systemctl is-active --quiet x-ui 2>/dev/null; then
    log "回滚完成且服务已 active：x-ui=$(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1)"
  else
    log "回滚后服务仍未 active，请人工介入（备份：$BACKUP_DIR）"
  fi
}
# 任何未被显式处理的失败都走回滚（显式分支里也调用，rollback 自身幂等）
trap 'rc=$?; if [[ $rc -ne 0 ]]; then rollback; fi' ERR

# ---------- 3. 下载并校验 ----------
log "准备面板包"
if [[ -n "${XUI_LOCAL:-}" ]]; then
  cp -f "$XUI_LOCAL" "$WORK/xui.tar.gz" || fail "复制本地面板包失败：$XUI_LOCAL"
else
  curl -fL --retry 3 --retry-delay 3 --connect-timeout 15 -o "$WORK/xui.tar.gz" "$XUI_URL" \
    || fail "下载面板包失败：$XUI_URL"
  if curl -fsL --retry 2 --connect-timeout 15 -o "$WORK/xui.sha256" "$XUI_SHA_URL" 2>/dev/null; then
    want="$(awk '{print $1}' "$WORK/xui.sha256")"
    got="$(sha256sum "$WORK/xui.tar.gz" | awk '{print $1}')"
    [[ "$want" == "$got" ]] || fail "面板包 sha256 不匹配（期望 $want 实际 $got）"
    log "面板包 sha256 校验通过"
  else
    log "警告：未取到 sha256 摘要，跳过完整性校验"
  fi
fi
mkdir -p "$WORK/xui" && tar xzf "$WORK/xui.tar.gz" -C "$WORK/xui" || fail "面板包解压失败"
[[ -f "$WORK/xui/x-ui/x-ui" ]] || fail "面板包结构异常"

log "准备 Xray $XRAY_VER"
if [[ -n "${XRAY_LOCAL:-}" ]]; then
  cp -f "$XRAY_LOCAL" "$WORK/xray.zip" || fail "复制本地 Xray 包失败：$XRAY_LOCAL"
else
  curl -fL --retry 3 --retry-delay 3 --connect-timeout 15 -o "$WORK/xray.zip" "$XRAY_URL" \
    || fail "下载 Xray 失败：$XRAY_URL"
fi
if [[ "$XRAY_ASSET" == *.zip ]]; then
  # 不依赖 unzip（最小云镜像常常没有）
  python3 -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' \
    "$WORK/xray.zip" "$WORK" || fail "Xray 包解压失败"
fi
[[ -f "$WORK/xray" ]] || fail "Xray 包结构异常"

# ---------- 4. 安装 ----------
systemctl stop x-ui || log "停止服务返回非 0（可能本来已停）"
install -m 755 "$WORK/xui/x-ui/x-ui" /usr/local/x-ui/x-ui || fail "安装面板二进制失败"
install -m 755 "$WORK/xray" "/usr/local/x-ui/bin/$XRAY_BIN" || fail "安装 Xray 失败"
for f in geosite.dat geoip.dat; do
  if [[ -f "$WORK/xui/x-ui/bin/$f" ]]; then
    cp -f "$WORK/xui/x-ui/bin/$f" "/usr/local/x-ui/bin/$f" || fail "更新 $f 失败"
  fi
done
systemctl start x-ui || fail "启动失败"

# ---------- 5. 验证（运行中的版本 / xray 进程 / 面板端口 / 入站） ----------
NEW_XUI="$(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1 || echo unknown)"
NEW_XRAY="$(/usr/local/x-ui/bin/$XRAY_BIN -version 2>/dev/null | head -1 || echo unknown)"
log "升级后：x-ui=$NEW_XUI | xray=$NEW_XRAY"
[[ "$NEW_XRAY" == *"$XRAY_PIN"* ]] || fail "Xray 版本不是 $XRAY_PIN（期望钉住的兼容版）"

for _ in $(seq 1 20); do
  if pgrep -f "bin/$XRAY_BIN" >/dev/null 2>&1; then break; fi
  sleep 1
done
pgrep -f "bin/$XRAY_BIN" >/dev/null 2>&1 || fail "xray 进程未启动（面板 active 但核心没起来）"
systemctl is-active --quiet x-ui || fail "x-ui 未处于 active"

PORT_INFO="$(python3 - <<'PY'
import sqlite3
c = sqlite3.connect('file:/etc/x-ui/x-ui.db?mode=ro', uri=True)
p = c.execute("select value from settings where key='webPort'").fetchone()
print(p[0] if p else '?')
PY
)" || PORT_INFO="?"
[[ "$PORT_INFO" != "?" ]] || fail "读不到面板端口（拒绝在无法验证的情况下宣告成功）"
# 面板在 xray 起来后才开始监听，探测要重试几秒，否则误判失败并触发回滚
panel_ok=0
for _ in $(seq 1 20); do
  if curl -sk -o /dev/null -m 5 "https://127.0.0.1:$PORT_INFO/" 2>/dev/null; then panel_ok=1; break; fi
  sleep 2
done
[[ $panel_ok -eq 1 ]] || fail "面板端口 $PORT_INFO 无响应（重试 20 次后）"

INB_AFTER="$(python3 - <<'PY'
import sqlite3
c = sqlite3.connect('file:/etc/x-ui/x-ui.db?mode=ro', uri=True)
print(','.join(f'{t}:{p}:{port}' for t, p, port in
      c.execute('select tag,protocol,port from inbounds where enable=1 order by id')))
PY
)"
[[ "$INB_BEFORE" == "$INB_AFTER" ]] || fail "启用入站集合发生变化：前=$INB_BEFORE 后=$INB_AFTER"

log "成功：x-ui=$NEW_XUI | xray=$NEW_XRAY | 面板端口=$PORT_INFO | 入站=$INB_AFTER"
log "注意：面板里的「更新 Xray」按钮会把核心换回新版，别点；本脚本是钉版的唯一保证"
log "备份保留最近 $KEEP_BACKUPS 份（$BACKUP_DIR）"
