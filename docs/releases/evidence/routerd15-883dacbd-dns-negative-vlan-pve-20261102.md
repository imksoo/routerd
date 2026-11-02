# Linux DNS 否定応答の分離 VLAN 実通信試験

## 結果

`883dacbd3df3f0725e07f2985ccfcf559089b3f4` の Linux パッケージで、ローカルゾーンと独立上流の存在しない名前が、独立クライアントから UDP/TCP とも `NXDOMAIN` になることを確認しました。

この記録は DNS の否定応答だけを対象にします。DNS 全機能、routerd 1.0 全機能、または製品全体の合格を示すものではありません。

## 候補と構成

- パッケージ: `dist/linux-amd64/routerd-linux-amd64.tar.gz`
- パッケージ SHA-256: `cd763b1a1356053dd6e7400358119a0edfbf23c674a16a4fd07ff580d7de83ad`
- VM219: 候補 `routerd`。VLAN 3131 の `198.18.15.1:53` で DNSResolver を待受。
- VM220: 独立した `dnsmasq` 上流。VLAN 3132 の `198.18.16.1:53` を使用。
- VM224: 独立した Linux クライアント。VLAN 3131 の `198.18.15.10/24` を使用。
- 新規 1.5 設定は、既存 VLAN アドレスを外部所有として扱い、DNSZone、DNSResolver、DNSForwarder、DNSUpstream、および防火壁だけを候補が所有。

## 観測結果

1. VM219 の候補は全宣言資源を `Healthy` とし、`198.18.15.1:53` の UDP/TCP リスナーを生成しました。
2. VM224 の `absent.home.test` は、UDP/TCP とも `NXDOMAIN` でした。
3. VM224 の `absent.upstream.test` は、UDP/TCP とも `NXDOMAIN` でした。
4. VM224 の `present.upstream.test` は、UDP/TCP とも `198.18.92.2` を取得しました。
5. VM220 の `dnsmasq` 記録では、存在・不存在の上流問い合わせが VM219 の `198.18.16.2` から到達していました。不存在名は VM220 側でも `NXDOMAIN` でした。

## 復元

VM219、VM220、VM224 を試験前スナップショットへ戻しました。試験用 ISO、スナップショット、VLAN 子インターフェース、候補プロセス、上流 DNS、一時ファイルを削除しました。最終状態は VM219・VM220 稼働、VM224 停止で、VM219・VM220 の基準 `routerd` は active です。

## 未確認範囲

- DNSSEC、DoT、DoH、名前空間封じ込め、全照会方針、DHCP リース由来名、キャッシュ失効、設定撤回・復旧、IPv6 DNS、および FreeBSD。
