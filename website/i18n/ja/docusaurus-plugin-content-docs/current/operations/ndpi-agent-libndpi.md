---
title: nDPI agent の native パッケージ
---

# nDPI agent の native パッケージ

routerd 1.5 提案版の追加 native nDPI agent は Debian パッケージ
`routerd-ndpi-agent-libndpi` として配布します。Linux 本体は `CGO_ENABLED=0` の
静的リンクを維持し、この追加 agent だけ `CGO_ENABLED=1 -tags libndpi` で OS の
共有ライブラリを使います。release build の対象は現在 Ubuntu 24.04 amd64 です。
1.5 の公開パッケージや apt リポジトリはまだ提供していません。

## 導入・更新

クリーン OS に、チェックサムを確認した同じ版の本体・追加パッケージを導入します。

```sh
sudo apt install ./routerd_<version>_amd64.deb \
  ./routerd-ndpi-agent-libndpi_<version>_amd64.deb
```

実際のファイル名を使ってください。本体の版を完全一致の依存として指定するため、
更新も本体と追加パッケージを一緒に行います。共有ライブラリの依存は実際の ELF と
OS のパッケージ情報から `dpkg-shlibdeps` で算出し、apt が解決します。
アーカイブや手動コピーでパッケージ管理下のファイルを上書きしません。

agent の実行パスは `/usr/sbin/routerd-ndpi-agent` のままです。
追加パッケージが所有する `dpkg-divert` により、本体の静的 agent は
`/usr/sbin/routerd-ndpi-agent.routerd-static` に振り分け、追加パッケージが実行パスを
所有します。本体更新で native agent を消しません。他のパッケージや管理者が
所有する振り分けは拒否し、引き取り・削除はしません。ソースや手動導入の移行処理ではありません。

追加パッケージも本体と同じ管理対象サービスの停止・復帰を使い、main と helper を
止めてからファイルを交換します。main を残すと、交換中に停止した helper を自動で
再起動し得るためです。追加パッケージだけの変更でも通信断やプロトコル再収束があり得ます。未設定の初回導入ではサービスを起動しません。

```sh
/usr/sbin/routerd-ndpi-agent selftest
sudo curl --fail --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

native 版の自己試験では `libndpiLoaded: true` が必要です。実ライブラリで TLS を
分類し、期待した SNI と `reason: libndpi_protocol_detection` を確認します。
実行中のソケットは設定が agent を要求した場合だけ存在します。導入成功だけでは
実通信の分類成功を意味しません。

## 追加パッケージの削除

最後の `FirewallEventLog` を削除または無効化すると、管理対象の収集プロセスを停止します。
有効な `FirewallEventLog` がなく、アプリケーション分類や TLS SNI を要求する
有効な `TrafficFlowLog` もなくなると、分類器とエージェントを停止し、生成した unit を撤去します。
停止の成功を確認してから unit を削除します。停止に失敗した場合は unit を残して再試行します。
リソースを再度有効化すると、導入済みパッケージを使って必要な unit を再生成します。

```sh
sudo apt remove routerd-ndpi-agent-libndpi
```

追加パッケージの実行ファイルと自分の振り分けを除去し、現在の本体パッケージの
静的 agent に戻します。稼働していた自分の agent/classifier はその実行ファイルで
復帰します。設定と DB は保持します。静的 agent は libnDPI をロードしないため、
native 分類が必要な運用中には追加パッケージを削除しないでください。

## ビルド・試験

対象 Linux ABI 上に Go、C compiler、`libndpi-dev`、`pkg-config`、`dpkg-dev`、
`dpkg-deb`、`file`、`readelf` を用意します。

```sh
make dist-ndpi-agent-libndpi-deb ROUTERD_OS=linux GOARCH=amd64 \
  PACKAGE_VERSION=<native-version>
go test ./tests/nativepackage
```

release workflow は `.deb`、実ライブラリの自己試験、基本 x86-64 ISA を検査します。
定義を追加しただけではリリースを実行したことにはなりません。

次の試験はクリーンな使い捨て systemd VM 専用です。root と apt 接続が必要です。
版番号が増え、実行ファイル自体も異なる新旧2組の本体・追加パッケージを渡します。

```sh
sudo sh scripts/linux-ndpi-deb-lifecycle-smoke.sh --isolated-vm \
  /tmp/base-prior.deb /tmp/base-current.deb \
  /tmp/addon-prior.deb /tmp/addon-current.deb
```

`TrafficFlowLog` が要求する生成サービス、独立 UDP/TCP DNS、native backend と TLS 分類、
追加パッケージだけの変更で 旧 main/DNS の終了と自動復帰、本体と一緒の更新、
ファイル検証、設定と SQLite の値の保持、更新済み静的 agent への復帰、最後の削除を
確認します。無関係な routerd 接頭辞のサービスも継続する必要があります。
結果を簡潔に記録してから VM・ディスク・成功ログを削除します。
転送中の実クライアントのパケット取得・分類や 1.0 全機能の合格を、この試験だけでは主張しません。

転送クライアントの分類は `routerctl get traffic-flows -o json` で確認します。
実際のTLS通信について `engine: ndpi-agent`、
`metadata.reason: libndpi_protocol_detection`、期待した `tlsSNI` を照合します。
firewallイベントは設定したサンプリングに依存し、flow確認の代用にはなりません。
DNS名は `dnsQuery`、HTTPのホスト名は `httpHost`、TLS/QUIC名は `tlsSNI` に表示します。
他のプロトコルのホスト名をSNIとして表示しません。
