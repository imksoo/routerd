---
title: トラブルシューティング
slug: /how-to/troubleshooting
---

# トラブルシューティング

routerd で問題が起きたときの基本の進め方です。

1. まず routerd 側の認識を見る: `routerctl describe <kind>/<name>`
2. ホスト側の状態 (`ip`、`ss`、`journalctl`) と比べる
3. `routerd apply --once --dry-run` で routerd が「何を直そうとしているか」を確認
4. 生成ファイルがおかしそうなら `routerd render` で原因を絞り込む
5. `/etc/...` を直接編集するのは最後の手段。直したらすぐ apply して routerd と同期を戻す

## 「apply は通ったがホストが変わらない」

routerd の apply は冪等です。生成ファイルが既に望む内容と一致していれば、
サービス再起動は走りません。再起動を期待していたのに起きていない場合:

```bash
sudo routerd render linux --config /usr/local/etc/routerd/router.yaml > /tmp/want.txt
diff /tmp/want.txt /etc/<actual-file>
```

diff が空ならホストは既に YAML どおり。空でなければ `journalctl -u routerd` の
権限エラーなどを見てください。

## 「DHCPv6-PD で全くプレフィックスが取れない」

調査順:

1. OS の DHCPv6 クライアントが動いているか
   - Linux: `networkctl status <wan-iface>`。 "DHCPv6 client: enabled" を確認
   - FreeBSD: `service dhcp6c status`。プロセスが active か確認
2. Solicit が wire に出ているか
   - `sudo tcpdump -ni <wan-iface> -nn -vv 'udp port 546 or udp port 547'`
3. Reply が wire に来ているか
   - 同じ tcpdump。Reply は送信元ポートが一時ポートのことがあるので、
     **`src port 547` でフィルタしないこと**
4. 経路が IPv6 マルチキャストを通しているか
   - Proxmox: `cat /sys/class/net/vmbr0/bridge/multicast_snooping` が `0`
   - L2 スイッチ: IGMP/MLD snooping を無効化、または MLD クエリアあり
5. 上流が実際に委譲しているか
   - NTT フレッツなら [FLET'S IPv6 設定](./flets-ipv6-setup) を参照

## 「再起動するとプレフィックスを失う」

routerd は直近観測したプレフィックスを `/var/lib/routerd/routerd.db` に記録します。
OS の DHCPv6 クライアントを再起動するときに Release を送ると、上流は binding を
即時解放することがあります。NTT プロファイルは既定で停止時の Release を抑止します。

クライアントが Release を送っていないかの確認:

- KAME `dhcp6c`: `rc.conf` に `dhcp6c_flags="-n"` が入っているか
- systemd-networkd: routerd が出すドロップインに `SendRelease=no` があるか

## 「routerctl describe が何も出さない」

`routerctl` はデーモンのローカルソケットと通信していて、YAML を直接見るわけでは
ありません。デーモンが動いていなければ describe は空になります。

```bash
sudo systemctl status routerd.service        # Linux
sudo service routerd status                  # FreeBSD
```

`routerd apply --once` は状態を更新しますがデーモンを起動はしません。one-shot 後に
状態を見たい場合は SQLite を直接読んでください
([運用: 状態データベース](../operations/state-database))。

## 「YAML を変えたのに apply が何もしない」

YAML の検証に落ちているか、変更対象のリソースが別のスコープに所有されている
(`Interface.spec.managed: false`、他ツールがファイルを作っている、など) のが
よくある原因です。次で確認:

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
sudo routerd apply --once --dry-run --config /usr/local/etc/routerd/router.yaml
```

ドライラン計画に「routerd が変更するもの」と「スキップするもの」が出ます。

## ログの場所

- Linux: `journalctl -u routerd.service`
- FreeBSD: `/var/log/messages` (`routerd[pid]` を探す)
- routerd の events テーブル:
  `sqlite3 /var/lib/routerd/routerd.db 'select * from events order by id desc limit 20'`
