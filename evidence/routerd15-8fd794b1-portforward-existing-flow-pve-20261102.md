# Linux PortForward 撤回時の既存接続試験

## 結果

`8fd794b1` の Linux 候補パッケージで、設定済み `PortForward` の撤回後も既存 TCP 接続が継続しました。
撤回後の新規接続は拒否され、同じ `PortForward` の再適用で新規接続も復旧しました。

## 実行対象

- ソースリビジョン: `8fd794b1eda0823a2bfeed6a479e95fd586bc1d7`
- パッケージ: `dist/linux-amd64/routerd-linux-amd64.tar.gz`
- パッケージ SHA-256: `9990c2008fab724f92a6b19ab52dff737da652c578c3b070c5c829f4afc85497`
- ビルド: `CGO_ENABLED=0 make dist`
- ローカル確認: `go test ./pkg/render ./pkg/controller/nat44 -count=1`
- 仮想マシン: VM219 が候補ルーター、VM220 が外部要求元、VM224 が内部応答器
- 分離経路: VLAN 3132 を WAN、VLAN 3131 を LAN として使用

## 観測

1. VM219 は `198.18.53.1/24` と `198.18.51.1/24` を保持し、`Healthy`、9 資源で起動しました。
2. VM220 の `198.18.53.2` から `198.18.53.1:18080` へ接続しました。
   VM224 の `198.18.51.10:8080` は `READY 198.18.53.2` を返しました。
3. 既存接続を開いたまま `PortForward` を除いた構成を `routerctl apply --replace` で適用しました。
   VM219 は `Healthy`、8 資源となり、`routerd_nat` には規則なしの `prerouting` と `postrouting` の NAT フックだけが残りました。
4. VM219 の接続追跡は `ESTABLISHED` を維持しました。
   VM220 の既存接続は `old-flow` に対して `ECHO old-flow` を受信し、`existing-flow.ok` を作成しました。
5. 同じ撤回状態で VM220 から開始した新規 TCP 接続は 3 秒で時間切れとなりました。
   `routerd_nat` に `routerd PortForward web` 規則はありませんでした。
6. ISO 上の読み取り専用の元構成を別名へ複製して再適用しました。
   VM219 は `Healthy`、9 資源に戻り、VM220 は再び `READY 198.18.53.2` を受信しました。

## 防火壁境界

撤回状態では WAN から LAN への `PortForward` 許可規則がなく、転送チェーンの既定方針は拒否でした。
候補ルーターに残った IPv4 アドレスは試験用の WAN と LAN の二つだけです。
NAT フックは既存接続の復路変換だけを維持し、新しい DNAT 規則を追加しません。

## 復元

VM219 と VM220 は試験前のスナップショットへ戻し、既存の `routerd` サービスが `active` であることを確認しました。
VM224 は試験前と同じ停止状態へ戻しました。
試験用 ISO、一時配置、三つの試験用スナップショットは削除済みです。

## 修正の根拠

撤回前の候補では、最後の NAT 規則を除くと `routerd_nat` 表全体が削除されました。
接続追跡は残っても復路の変換フックが消え、既存 TCP 接続がリセットされました。
`8fd794b1` は既存接続が終了するまで空の IPv4 NAT フックを保持します。

## 範囲外

`NAT44FlowDNATPinhole`、`NAT44SessionSync`、`LocalServiceRedirect`、UDP、IPv6 NAT、再起動後の接続維持、物理機器での試験は未確認です。
この結果は NAT 機能全体、Linux 全体、routerd 1.0 の全機能、または置換可能品質の合格を意味しません。
