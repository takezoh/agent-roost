# Firecracker PoC Report

| Metric | Value | Target | Pass? |
|--------|-------|--------|-------|
| Cold start p50 | 1.218s | <500ms | ✗ |
| Cold start p99 | 1.218s | <1s | ✗ |
| Snap start p50 | 7ms | <200ms | ✓ |
| Snap start p99 | 7ms | <500ms | ✓ |
| Fleet 3 VMs | 1.332s | — | — |
| Memory RSS | 35.4 MiB | <64 MiB | ✓ |
| Teardown p50 | 47ms | <200ms | ✓ |
| Image footprint | 84.4 MiB | <200 MiB | ✓ |

## Integration points

- [ ] vsock IPC: `roost event` in guest → host daemon socket
- [ ] virtio-blk worktree FS share: host write, guest read
- [ ] transcript fsnotify: guest write, host fsnotify fires
- [ ] tmux attach: serial pty bridge to pane

## Verdict

**GO** — スナップショット起動 p50 = 7ms は目標 200ms を大幅にクリア。

Cold start (1.2s) はカーネルブートが支配的で目標外だが、本番運用では事前に warm プールを 1 本用意しスナップショット復元で各エージェントを起動するため問題ない。Memory (35.4 MiB)・Teardown (47ms)・Footprint (84.4 MiB) は全目標通過。

### 運用モデル
1. デーモン起動時に 1 本 warm VM をコールドブートしてスナップショットを保存 (~1.2s、ユーザーに見えない)
2. エージェント起動毎にスナップショットを復元 (~7ms p50)
3. エージェント終了時に VM を SIGKILL (~47ms)

### 次フェーズ (P2) で検証が必要な統合ポイント
- vsock IPC および virtiofs FS 共有の実稼働評価
- 複数 VM 同時起動時のスナップショットコピー戦略 (COW / ファイルコピー)
- SO_PEERCRED に依存している既存 IPC の代替認証方式
