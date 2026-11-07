---
title: 導入と更新
---

# 導入と更新

routerd 1.5 提案版は、クリーンインストールした OS に Debian/Ubuntu の `.deb`、
FreeBSD の `.pkg` で導入します。現在開発中であり、従来の stable release の
アーカイブは 1.5 のパッケージではありません。公開 apt/pkg リポジトリもまだ
案内していません。対象ビルド・OS・CPU に合うパッケージを取得し、同梱の
チェックサムを確認してください。

パッケージから次のパッケージへの更新を対象とします。1.0 の設定、`make install`、
手動コピー、アーカイブ導入済み環境の検出・移行は対象外です。
1.5 用に設定を用意し、クリーンな OS に新規導入してください。

## 導入

Debian/Ubuntu では、ローカルのパッケージを apt に渡します。
依存パッケージは OS に設定されたリポジトリから解決します。

```sh
sudo apt install ./routerd_<version>_amd64.deb
```

FreeBSD 14 では pkg を使います。

```sh
sudo pkg -o HANDLE_RC_SCRIPTS=true install ./routerd-<version>.pkg
```

プレースホルダーは取得した実際のファイル名に置き換えてください。
arm64 ホストには arm64 用を使います。FreeBSD の ABI 名では `aarch64` です。
Linux の routerd バイナリは `CGO_ENABLED=0` で静的リンクしますが、OS の実行ツール
は別途必要です。FreeBSD の標準パッケージには native libnDPI agent と実ライブラリの依存を含めます。
基本依存はパッケージ情報で宣言します。追加機能が要求する
OS パッケージも、選択した設定に合わせて適用前に確認してください。

初回導入ではファイルとサービス定義を配置するだけで、daemon の起動、ルーティングの
有効化、設定例の適用はしません。実際の運用設定や DB はパッケージに含めません。

| 用途 | Debian/Ubuntu | FreeBSD |
| --- | --- | --- |
| daemon | `/usr/sbin/routerd` | `/usr/local/sbin/routerd` |
| 設定 | `/etc/routerd/router.yaml` | `/usr/local/etc/routerd/router.yaml` |
| 設定例 | `/usr/share/doc/routerd/examples` | `/usr/local/share/doc/routerd/examples` |
| 永続状態 | `/var/lib/routerd` | `/var/db/routerd` |
| 実行時ソケット | `/run/routerd` | `/var/run/routerd` |
| サービス | vendor systemd unit | `/usr/local/etc/rc.d/routerd` |

## 設定と起動

インターフェース名、アドレス、DHCP/DNS、NAT、ファイアウォールを確認してから
起動します。コンソールか独立した管理経路を確保してください。同梱の大きな設定例は
参照資料であり、どの環境でも安全に使える既定値ではありません。

```sh
# Debian/Ubuntu
sudo routerd validate --config /etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo systemctl status routerd.service

# FreeBSD
sudo routerd validate --config /usr/local/etc/routerd/router.yaml
sudo sysrc routerd_enable=YES
sudo service routerd start
sudo service routerd status
```

設定済み daemon の起動はネットワーク状態を変更します。構文確認後に起動し、
実クライアントの通信、失敗時の挙動、復旧を確認してください。
`routerctl get status` で実行状態を確認できますが、導入成功だけでは通信の成功を
証明できません。

## パッケージ更新

運用設定と永続状態は上記の場所で保持します。初回導入と同じ apt/pkg コマンドで
新しいローカルパッケージを導入します。配布ファイルはパッケージ管理下に置き、
`make install` や手動コピーで上書きしないでください。

Debian の更新処理は起動していた自分のサービスを記録し、旧 daemon と補助サービスを
停止して置換後に復帰させます。以前停止していた Debian サービスは停止したままです。
FreeBSD は pkg 標準の `HANDLE_RC_SCRIPTS=true` を使います。稼働ルーターは
`routerd_enable=YES` とし、更新時も上記コマンドのオプションを付けてください。
pkg の hook 後処理が終わってから有効な rc.d サービスを起動し、main の rc.d 起動処理が
以前稼働していた自分の補助サービスを復帰させます。FreeBSD では、有効化済みなら
更新直前に停止していても起動する場合があります。オプションはその pkg 実行だけに適用し、
グローバル設定は変更しません。指定しない場合は更新後に停止したままになる可能性があります。
hook からの直接起動は pkg が子孫プロセスを終了させるため使いません。
新規導入で未有効のサービスは起動しません。設定と DB は保持します。
更新時には通信断やプロトコルの再収束が起こり得るため、更新後は独立した
クライアントから通信を確認してください。DB 形式の移行契約や BGP の無停止更新を
保証するものではありません。

旧サービスを停止できなければ更新前処理で置換を拒否します。パッケージ管理の
診断と実サービスの状態を確認し、終了コードだけで復旧を判断しないでください。
Debian/Ubuntu のサービス起動ポリシーで再起動が禁止されている場合は、その方針を
解決する必要があります。再収束中の daemon を、確認を通すために繰り返し再起動
しないでください。

## 削除

```sh
# Debian/Ubuntu
sudo apt remove routerd

# FreeBSD
sudo pkg delete routerd
```

自分のサービスを停止し、配布ファイルと所有マーカーのある生成サービスを削除します。
運用設定と永続 DB は Debian の purge を含めて保持します。削除は、適用済みの
ネットワーク状態すべてを元に戻す保証ではありません。撤去前に設定上のリソースを
取り下げ、通信とカーネル状態を確認してください。保持した設定・状態の削除は、
運用者が明示的に破棄する場合だけ行います。

## ビルドと検証

Linux は `make dist-deb ROUTERD_OS=linux`、FreeBSD 実機では
`gmake dist-pkg ROUTERD_OS=freebsd` を使います。リリース成果物では `GOARCH` と
`PACKAGE_VERSION` を指定します。FreeBSD の生成には native `pkg`、Python、
`dnsmasq`、`ca_root_nss`、`ndpi`、C compiler、`pkgconf`、`readelf`、Linux には `dpkg-deb` と Python が必要です。
release workflow は amd64/arm64 の両方を生成しますが、定義を追加しただけでは
リリース実行や両 CPU の実行時検証が済んだことにはなりません。

再実行できる導入・更新・削除試験と合否基準は[開発手順](./operations/development.md)
にあります。使い捨て OS ゲストで実際の置換、独立した DNS 疎通、旧プロセス終了、
設定と SQLite の値の保持、削除を確認します。全機能の認証ではありません。
Linux の native libnDPI は本体と同じ版の[追加パッケージ](./operations/ndpi-agent-libndpi.md)を使います。パッケージ管理下の
agent をアーカイブで上書きしないでください。

## 設定適用中断と保存世代への復旧確認

正式 `2da31049` Linux パッケージで、隔離 VM 上の設定確定後・適用前のプロセス停止を確認しました。同じ所有リソース名でアドレスだけを変えた候補を `--no-reconcile` で確定し、主 daemon を SIGKILL すると、systemd が自動再起動して正本の新アドレスを適用し、旧アドレスを撤去しました。DB の初期化や手動起動は行っていません。続く `routerctl rollback --timeout 90s --to <保存世代>` で正本と元のアドレスが復元され、`routerctl delete` によるアドレス撤去後も別の設定済みサービスは応答しました。

復旧確認では返された世代と phase、正本の内容、実際の OS 効果を照合します。同じ設定への冪等な適用では mtime や checksum が変わらない場合があります。要求がタイムアウトした場合も、再試行を決める前に正本と実行状態を確認します。バイナリを戻す場合は APT を使い、nDPI addon 導入済みなら同じ版の base と addon を同じトランザクションで指定します。詳しい手順は [復旧手順](operations/release-rollback.md) を参照してください。
