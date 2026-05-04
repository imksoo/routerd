# Path MTU と TCP MSS

`PathMTUPolicy` は、routerd が RA と TCP MSS 調整に使う MTU の前提を管理します。

トンネル経路では、静的な MTU 値だけでは壊れやすくなります。DS-Lite、PPPoE、オーバーレイでは、実際に使えるパケットサイズが変わるためです。`mtu.source: probe` を使うと、routerd は経路 MTU を測定します。その結果から nftables の MSS 調整テーブルを作り直します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PathMTUPolicy
metadata:
  name: lan-to-dslite-mtu
spec:
  fromInterface: lan
  toInterfaces:
    - ds-lite-a
    - ds-lite-b
    - ds-lite-c
  mtu:
    source: probe
    value: 1454
    probe:
      family: ipv4
      targets:
        - 1.1.1.1
        - 8.8.8.8
      min: 1280
      max: 1500
      fallback: 1454
      interval: 10m
      timeout: 1s
  interfaceMTU:
    enabled: true
  tcpMSSClamp:
    enabled: true
    families:
      - ipv4
```

Linux では DF 付きの `ping` で測定します。routerd は各送信先インターフェースを確認し、成功した値のうち最小の MTU を使います。すべて失敗した場合は `fallback` を使います。

測定値は `interval` の間だけ再利用します。これにより、コントローラーの調整ごとに能動的な測定が走ることを避けます。

`interfaceMTU.enabled` が true の場合、routerd は送信先インターフェースの MTU も測定値へ下げます。これはトンネルインターフェースで有効です。UDP や TCP 以外の通信にも同じパケットサイズ上限を見せられます。

IPv4 TCP では MSS を `MTU - 40` にします。IPv6 TCP では MSS を `MTU - 60` にします。
