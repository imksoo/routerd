# MobilityPool

[リソース別の設定](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

共有SAMトポロジー・ローカル所有権観測・BGPと連携状態を組み合わせ、アドレス移動とローカル取得を計画します。YAMLは自ノードの要求で、共有識別情報はSAMNodeSetに置きます。

## 指定条件と参照関係

- prefixはIPv4が必要です。groupRefはnodeNameを持つローカルEventGroupへ解決し、その自ノードが必須membersFromトポロジー内に必要です。全メンバーは非空siteとonprem・cloudロールが必要で、取得・探索・静的所有上書きは自ノードだけです。
- onprem proxy-ARPはcapture.interfaceと明示activeWhenのsingle-router・vrrp-masterが必要です。プロバイダー取得はproviderRefと、探索時を除きnicRefが必須で、route-tableはtarget.routeTableRefも必要です。プロバイダー取得のactiveWhenは禁止です。
- Linuxはインターフェース単位proxy-ARP、FreeBSDはLinux sysctl名ではなく経路に従う全体proxyallで制御します。取得有効化は実所有・準備状態にも依存し、YAML検証・計画だけで拡張セグメントの開通とは判定しません。
- プロファイルは非ゼロ値でマージし、空文字・空一覧・falseで継承項目を解除しません。送信元固定値は状態参照より優先し、mapはローカル値を優先してマージし、target固定値をtargetFromより優先します。プロファイル利用はcloud限定です。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    members: # 任意; array
      - # object
        capture: # 任意; object
          activeWhen: # 任意; object
            type: "" # 任意; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 任意; string
          captureStrategy: "" # 任意; string; enum=["", "route-table"]
          excludeAddresses: # 任意; array
            - "<string>" # string
          gratuitousARP: true # 任意; boolean
          interface: "<string>" # 任意; string
          nicRef: "<string>" # 任意; string
          providerRef: "<string>" # 任意; string
          sourceAddress: "<string>" # 任意; string
          sourceAddressFrom: # 任意; object
            field: "<string>" # 任意; string
            optional: true # 任意; boolean
            resource: "<string>" # 必須; string
          target: # 任意; object
            "<key>": "<string>" # 任意; string
          targetFrom: # 任意; object
            "<key>": "<string>" # 任意; string
          type: "provider-secondary-ip" # 任意; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必須; string
        ownershipDiscovery: # 任意; object
          allowEmptyAfter: "<string>" # 任意; string
          leaseTTL: "<string>" # 任意; string
          mode: "" # 任意; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 任意; string
          providerRef: "<string>" # 任意; string
          scanInterval: "<string>" # 任意; string
          scope: # 任意; object
            excludeAddresses: # 任意; array
              - "<string>" # string
            includeAddresses: # 任意; array
              - "<string>" # string
          selector: # 任意; object
            tags: # 任意; object
              "<key>": "<string>" # 任意; string
          sources: # 任意; array
            - # object
              bridge: "<string>" # 任意; string
              interface: "<string>" # 任意; string
              leaseTTL: "<string>" # 任意; string
              network: "<string>" # 任意; string
              probeRetries: 0 # 任意; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 任意; string
              resource: "<string>" # 任意; string
              scanInterval: "<string>" # 任意; string
              sourceAddressFrom: # 任意; object
                field: "<string>" # 任意; string
                optional: true # 任意; boolean
                resource: "<string>" # 必須; string
              type: "dhcpv4-lease" # 必須; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 任意; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 任意; string
          subnetRefFrom: "<string>" # 任意; string
        profileRef: "<string>" # 任意; string
        staticOwnedAddresses: # 任意; array
          - "<string>" # string
    membersFrom: # 必須; array; minItems=1
      - # object
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    prefix: "<string>" # 必須; string
    profiles: # 任意; object
      cloudCaptures: # 任意; object
        "<key>": # 任意; object
          capture: # 任意; object
            activeWhen: # 任意; object
              type: "" # 任意; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 任意; string
            captureStrategy: "" # 任意; string; enum=["", "route-table"]
            excludeAddresses: # 任意; array
              - "<string>" # string
            gratuitousARP: true # 任意; boolean
            interface: "<string>" # 任意; string
            nicRef: "<string>" # 任意; string
            providerRef: "<string>" # 任意; string
            sourceAddress: "<string>" # 任意; string
            sourceAddressFrom: # 任意; object
              field: "<string>" # 任意; string
              optional: true # 任意; boolean
              resource: "<string>" # 必須; string
            target: # 任意; object
              "<key>": "<string>" # 任意; string
            targetFrom: # 任意; object
              "<key>": "<string>" # 任意; string
            type: "provider-secondary-ip" # 任意; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 任意; object
            allowEmptyAfter: "<string>" # 任意; string
            leaseTTL: "<string>" # 任意; string
            mode: "" # 任意; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 任意; string
            providerRef: "<string>" # 任意; string
            scanInterval: "<string>" # 任意; string
            scope: # 任意; object
              excludeAddresses: # 任意; array
                - "<string>" # string
              includeAddresses: # 任意; array
                - "<string>" # string
            selector: # 任意; object
              tags: # 任意; object
                "<key>": "<string>" # 任意; string
            sources: # 任意; array
              - # object
                bridge: "<string>" # 任意; string
                interface: "<string>" # 任意; string
                leaseTTL: "<string>" # 任意; string
                network: "<string>" # 任意; string
                probeRetries: 0 # 任意; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 任意; string
                resource: "<string>" # 任意; string
                scanInterval: "<string>" # 任意; string
                sourceAddressFrom: # 任意; object
                  field: "<string>" # 任意; string
                  optional: true # 任意; boolean
                  resource: "<string>" # 必須; string
                type: "dhcpv4-lease" # 必須; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 任意; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 任意; string
            subnetRefFrom: "<string>" # 任意; string
    staticHandovers: # 任意; array
      - # object
        address: "<string>" # 必須; string
        fromNodeRef: "<string>" # 必須; string
        toNodeRef: "<string>" # 必須; string
    values: # 任意; object
      "<key>": "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.prefix` | 必須の管理IPv4 CIDRです。所有・取得アドレスはこの範囲内で、既定prefix・アドレス自動割当はありません。 |
| `spec.groupRef` | nodeNameを持つ必須ローカルEventGroup参照です。その自ノード識別子がmembersFrom内に必要で、所有・失効イベントに使います。 |
| `spec.values` | プロバイダー識別子用の任意文字列定数mapです。targetFrom・subnetRefFromが参照し、自動変数展開・秘密保存は行いません。 |
| `spec.profiles` | 任意の再利用するローカル取得・探索既定値です。共有ノード識別は含めません。 |
| `spec.profiles.cloudCaptures` | プロファイル名をキーにするmapです。自cloudメンバーのprofileRefで使い、onpremの短縮指定には使えません。未選択プロファイルもtarget・values参照を正規化します。 |
| `spec.membersFrom` | 必須の非空SAMNodeSet参照元一覧です。識別・site・role・placement・maintenance・容量の唯一の宣言元で、members上書きからトポロジーを補完しません。 |
| `spec.membersFrom[].resource` | 必須のSAMNodeSet/nameです。取り込むnodeRefは重複不可で、各解決メンバーのsite・onpremまたはcloudロールを明示します。 |
| `spec.membersFrom[].optional` | 既定falseです。非optional参照元欠落は実行時Pendingで、trueなら許容できますが、EventGroupの自ノードをトポロジー内で解決する条件は省略しません。 |
| `spec.members` | membersFromから取り込んだnodeRefへの任意の自ノード上書きです。共有site・role・placement・maintenance・容量や、リモート取得を設定できません。 |
| `spec.members[].nodeRef` | EventGroup自ノードを選ぶ必須の取り込み済みnodeRefです。新メンバーを導入する指定ではありません。 |
| `spec.members[].profileRef` | profiles.cloudCapturesの任意キーで、cloud限定です。欠落は正規化失敗です。非空文字列・一覧を上書きしmapをマージしますが、trueは加算し、空一覧・falseで継承値を解除できません。 |
| `spec.members[].staticOwnedAddresses` | prefix内の任意の明示IPv4 /32 CIDRで、onprem限定です。ここでは裸IPv4は拒否し、静的所有重複も不可です。学習端末イベントに依存しない所有宣言です。 |
| `spec.staticHandovers` | 任意の計画的な静的所有権移動です。移動元の解放を観測してから移動先へ投影し、同時二重所有にはしません。 |
| `spec.staticHandovers[].address` | 必須のプール内IPv4 /32 CIDRで、handover間で一意です。静的所有宣言がある場合、その所有者はfromNodeRefに一致が必要です。 |
| `spec.staticHandovers[].fromNodeRef` | 必須の既存取り込み済みonpremメンバーnodeRefで、toNodeRefと異なる必要があります。 |
| `spec.staticHandovers[].toNodeRef` | 必須の既存取り込み済み移動先nodeRefで、移動元と異なる必要があります。移動先の自動作成はしません。 |
| `spec.members[].capture` | ローカル取得設定です。typeはroleから推定せず明示します。取得・探索の上書きは自ノードのみです。 |
| `spec.members[].capture.type` | onpremはproxy-arp、cloudはprovider-secondary-ipです。空なら取得方式なしですが、自cloudメンバーは解決した取得設定が必要です。 |
| `spec.members[].capture.providerRef` | provider-secondary-ipで必須のCloudProviderProfile名です。状態参照ではありません。所有権探索providerRefの既定にも使います。 |
| `spec.members[].capture.captureStrategy` | 任意のroute-tableだけをprovider-secondary-ipで指定でき、target.routeTableRefが必須です。通常のsecondary-IP・proxy-ARPは省略し、strategyにsecondary-ip・proxy-arpを指定すると拒否します。 |
| `spec.members[].capture.nicRef` | プロバイダーNIC識別子です。provider-private-ip探索から解決する場合以外、プロバイダー取得で必須です。target・targetFrom内のnicRefは禁止です。 |
| `spec.members[].capture.interface` | proxy-arpで必須の論理・OS取得インターフェースです。利用側で別名を解決し、LANの自動選択はしません。 |
| `spec.members[].capture.sourceAddress` | proxy-ARP・BGP配送に使うプール内ローカルIPv4送信元です。固定値はsourceAddressFromより優先し、ここでは排他検証しません。有効なら/32アドレス・優先送信元へ展開し、リモート所有者のアドレスではありません。 |
| `spec.members[].capture.sourceAddressFrom` | 送信元アドレスの任意状態参照です。有効なプール内IPv4値を使い、欠落・不正値は未解決です。固定値と異なり参照元がアドレス寿命を持つためIPv4StaticAddressを生成しません。 |
| `spec.members[].capture.excludeAddresses` | 任意のproxy-ARP取得対象外プール内IPv4アドレス・CIDRで、既定なしです。ローカル限定ゲートウェイ等に使い、BGP全経路のフィルターではありません。 |
| `spec.members[].capture.gratuitousARP` | 既定falseです。trueは取得変更のARP通知を要求します。プロファイル継承は加算的で、ローカルfalseではプロファイルtrueを解除できません。 |
| `spec.members[].capture.activeWhen` | onprem proxy-arpで必須の明示的な権限条件です。provider-secondary-ipでは禁止で、汎用state・all・any条件ではありません。 |
| `spec.members[].capture.activeWhen.type` | single-routerはローカルproxy-ARPを無条件許可し、vrrp-masterは参照VirtualAddressのMASTER状態を要求します。proxy-ARPでは既定なしです。 |
| `spec.members[].capture.activeWhen.virtualAddressRef` | vrrp-masterでは必須の既存ローカルVirtualAddress名（VirtualAddress/name表記も対応）です。single-routerでは禁止で、自メンバーだけを検証します。 |
| `spec.members[].capture.target` | 任意の非秘密プロバイダー対象mapです。空キー・値と秘密・token・password・credential・private-key・access-key類のキーは禁止し、明示値をtargetFromより優先します。 |
| `spec.members[].capture.targetFrom` | プロバイダー対象キーからspec.valuesキーへの任意mapです。参照定数の欠落・空値は正規化を失敗させます。nicRefは禁止でcapture.nicRefを使います。 |
| `spec.members[].capture.sourceAddressFrom.resource` | 参照元Kind/nameで、共通検証によりリソース存在が必要です。 |
| `spec.members[].capture.sourceAddressFrom.field` | resource指定時に必須の参照元提供フィールドです。 |
| `spec.members[].capture.sourceAddressFrom.optional` | 既定falseです。参照検証を省略せず、欠落時の送信元補完もしません。 |
| `spec.members[].ownershipDiscovery` | ローカル所有権の事実探索設定で、既定無効です。自ノードだけが設定し、受信するリモートイベントは別入力です。 |
| `spec.members[].ownershipDiscovery.mode` | 空・disabledは処理なしです。provider-private-ipはcloud・provider-secondary-ip、onprem-l2はonprem・proxy-arp・capture.interface・非空sourcesが必要です。 |
| `spec.members[].ownershipDiscovery.providerRef` | プロバイダー探索プロファイル名で、既定はcapture.providerRefです。provider-private-ipで使い、イベントリソース参照ではありません。 |
| `spec.members[].ownershipDiscovery.pluginRef` | 任意の探索Plugin名です。空ならプロバイダー名または唯一のobserve.providerPrivateIPs対応プラグインで解決し、曖昧・能力欠落は探索失敗です。 |
| `spec.members[].ownershipDiscovery.subnetRef` | 任意のプロバイダーサブネット識別子です。明示値をsubnetRefFromより優先し、空なら対応プラグインが自NICから推定します。 |
| `spec.members[].ownershipDiscovery.subnetRefFrom` | subnetRefが空のときだけ使う任意のspec.valuesキーです。定数の欠落・空値は正規化失敗で、Kind/name・field形式ではありません。 |
| `spec.members[].ownershipDiscovery.scanInterval` | プロバイダー探索間隔で、既定60秒、明示値30秒以上です。onprem-l2でも検証しますがイベント入力ではプロバイダー探索は行わず、観測pollingはsource別scanIntervalです。 |
| `spec.members[].ownershipDiscovery.leaseTTL` | 所有権イベントの正のGo durationで、既定5分です。onprem観測はsource別leaseTTLを優先します。所有権リースの期限で、再起動の合否時間ではありません。 |
| `spec.members[].ownershipDiscovery.stoppedInstancePolicy` | provider-private-ip専用です。hold・省略は停止インスタンスの所有事実を保持し、releaseは失効させます。非空値は他modeで禁止です。 |
| `spec.members[].ownershipDiscovery.allowEmptyAfter` | onprem-l2専用の正の期間です。観測開始後この期間を経て端末0件を完了扱いにでき、省略時は保留です。profileRef利用時は現行マージがこの項目のローカル上書きをコピーせず、プロファイル値が残ります。 |
| `spec.members[].ownershipDiscovery.sources` | onprem-l2で必須の非空一覧で、provider-private-ipでは禁止です。type・resource・実効interface・network・bridgeの同一組み合わせは拒否します。 |
| `spec.members[].ownershipDiscovery.sources[].type` | 必須のdhcpv4-lease・arp-observer・on-demand-arp・pve-svnetです。ARP・PVEはローカル観測デーモンを生成し、DHCPは実リースイベントを使います。 |
| `spec.members[].ownershipDiscovery.sources[].resource` | 任意の参照元識別文字列で、既定空です。重複・観測識別fingerprintに使い、観測プロセス名や存在検証付きKind/name参照ではありません。イベント照合は主にtype・interface・network・bridgeです。 |
| `spec.members[].ownershipDiscovery.sources[].interface` | 任意の観測インターフェースで、既定capture.interfaceです。on-demand-arpでは補完後に必須で、利用側が宣言別名をOS名へ解決します。 |
| `spec.members[].ownershipDiscovery.sources[].bridge` | PVE・svnet観測照合に使う任意のローカルbridgeラベルです。既定空はbridge制限なしで、Bridge作成ではありません。 |
| `spec.members[].ownershipDiscovery.sources[].network` | svnet1等の任意のPVE・svnet論理networkラベルです。空ならnetwork選択条件なしで、IP CIDRではありません。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom` | アクティブARP送信元の任意状態参照です。欠落・利用不能なら検証済み取得送信元へ戻り、どちらも解決しなければon-demand probeを生成しません。 |
| `spec.members[].ownershipDiscovery.sources[].scanInterval` | 任意の観測poll間隔で、明示値1秒以上、生成ARP・PVEデーモンの既定10秒です。イベント駆動DHCPではpollしません。 |
| `spec.members[].ownershipDiscovery.sources[].probeTimeout` | アクティブARP試行間の正のGo durationで、観測デーモン既定1秒です。on-demand probingで使います。 |
| `spec.members[].ownershipDiscovery.sources[].probeRetries` | 0～20の追加ARP再試行数で、既定0です。総試行回数ではなく初回1回は別です。 |
| `spec.members[].ownershipDiscovery.sources[].leaseTTL` | 任意の正のイベント寿命で、このsourceのownershipDiscovery.leaseTTLを上書きします。省略時は探索リース（既定5分）を継承します。 |
| `spec.members[].ownershipDiscovery.scope` | プール内の任意の所有権観測アドレス制限です。外部prefixを任意に広告する認可ではありません。 |
| `spec.members[].ownershipDiscovery.scope.includeAddresses` | プール内IPv4アドレス・CIDR許可一覧です。空なら全候補を許可し、有効シャード割当はプロバイダー探索のこの一覧を置き換えます。 |
| `spec.members[].ownershipDiscovery.scope.excludeAddresses` | 任意のプール内IPv4アドレス・CIDR除外一覧で、既定なしです。許可一覧の照合後に除外します。 |
| `spec.members[].ownershipDiscovery.selector` | 任意のプロバイダー探索タグ条件です。クラウドのタグ設定を変更しません。 |
| `spec.members[].ownershipDiscovery.selector.tags` | 空でないタグキーと期待文字列のmapです。前後空白除去後の値が全キーで一致する必要があり、空mapなら制限なしです。プロバイダー探索用でonpremイベント照合には使いません。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.resource` | 状態参照利用時に必須の既存Kind/nameです。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.field` | resource利用時必須の参照元提供フィールドです。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.optional` | 既定falseです。参照検証を省略せず、未解決の送信元アドレスも補完しません。 |
| `spec.profiles.cloudCaptures.<name>.capture` | ローカル取得設定です。typeはroleから推定せず明示します。取得・探索の上書きは自ノードのみです。 |
| `spec.profiles.cloudCaptures.<name>.capture.type` | onpremはproxy-arp、cloudはprovider-secondary-ipです。空なら取得方式なしですが、自cloudメンバーは解決した取得設定が必要です。 |
| `spec.profiles.cloudCaptures.<name>.capture.providerRef` | provider-secondary-ipで必須のCloudProviderProfile名です。状態参照ではありません。所有権探索providerRefの既定にも使います。 |
| `spec.profiles.cloudCaptures.<name>.capture.captureStrategy` | 任意のroute-tableだけをprovider-secondary-ipで指定でき、target.routeTableRefが必須です。通常のsecondary-IP・proxy-ARPは省略し、strategyにsecondary-ip・proxy-arpを指定すると拒否します。 |
| `spec.profiles.cloudCaptures.<name>.capture.nicRef` | プロバイダーNIC識別子です。provider-private-ip探索から解決する場合以外、プロバイダー取得で必須です。target・targetFrom内のnicRefは禁止です。 |
| `spec.profiles.cloudCaptures.<name>.capture.interface` | proxy-arpで必須の論理・OS取得インターフェースです。利用側で別名を解決し、LANの自動選択はしません。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddress` | proxy-ARP・BGP配送に使うプール内ローカルIPv4送信元です。固定値はsourceAddressFromより優先し、ここでは排他検証しません。有効なら/32アドレス・優先送信元へ展開し、リモート所有者のアドレスではありません。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom` | 送信元アドレスの任意状態参照です。有効なプール内IPv4値を使い、欠落・不正値は未解決です。固定値と異なり参照元がアドレス寿命を持つためIPv4StaticAddressを生成しません。 |
| `spec.profiles.cloudCaptures.<name>.capture.excludeAddresses` | 任意のproxy-ARP取得対象外プール内IPv4アドレス・CIDRで、既定なしです。ローカル限定ゲートウェイ等に使い、BGP全経路のフィルターではありません。 |
| `spec.profiles.cloudCaptures.<name>.capture.gratuitousARP` | 既定falseです。trueは取得変更のARP通知を要求します。プロファイル継承は加算的で、ローカルfalseではプロファイルtrueを解除できません。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen` | onprem proxy-arpで必須の明示的な権限条件です。provider-secondary-ipでは禁止で、汎用state・all・any条件ではありません。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.type` | single-routerはローカルproxy-ARPを無条件許可し、vrrp-masterは参照VirtualAddressのMASTER状態を要求します。proxy-ARPでは既定なしです。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.virtualAddressRef` | vrrp-masterでは必須の既存ローカルVirtualAddress名（VirtualAddress/name表記も対応）です。single-routerでは禁止で、自メンバーだけを検証します。 |
| `spec.profiles.cloudCaptures.<name>.capture.target` | 任意の非秘密プロバイダー対象mapです。空キー・値と秘密・token・password・credential・private-key・access-key類のキーは禁止し、明示値をtargetFromより優先します。 |
| `spec.profiles.cloudCaptures.<name>.capture.targetFrom` | プロバイダー対象キーからspec.valuesキーへの任意mapです。参照定数の欠落・空値は正規化を失敗させます。nicRefは禁止でcapture.nicRefを使います。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.resource` | 参照元Kind/nameで、共通検証によりリソース存在が必要です。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.field` | resource指定時に必須の参照元提供フィールドです。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.optional` | 既定falseです。参照検証を省略せず、欠落時の送信元補完もしません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery` | ローカル所有権の事実探索設定で、既定無効です。自ノードだけが設定し、受信するリモートイベントは別入力です。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.mode` | 空・disabledは処理なしです。provider-private-ipはcloud・provider-secondary-ip、onprem-l2はonprem・proxy-arp・capture.interface・非空sourcesが必要です。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.providerRef` | プロバイダー探索プロファイル名で、既定はcapture.providerRefです。provider-private-ipで使い、イベントリソース参照ではありません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.pluginRef` | 任意の探索Plugin名です。空ならプロバイダー名または唯一のobserve.providerPrivateIPs対応プラグインで解決し、曖昧・能力欠落は探索失敗です。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRef` | 任意のプロバイダーサブネット識別子です。明示値をsubnetRefFromより優先し、空なら対応プラグインが自NICから推定します。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRefFrom` | subnetRefが空のときだけ使う任意のspec.valuesキーです。定数の欠落・空値は正規化失敗で、Kind/name・field形式ではありません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scanInterval` | プロバイダー探索間隔で、既定60秒、明示値30秒以上です。onprem-l2でも検証しますがイベント入力ではプロバイダー探索は行わず、観測pollingはsource別scanIntervalです。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.leaseTTL` | 所有権イベントの正のGo durationで、既定5分です。onprem観測はsource別leaseTTLを優先します。所有権リースの期限で、再起動の合否時間ではありません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.stoppedInstancePolicy` | provider-private-ip専用です。hold・省略は停止インスタンスの所有事実を保持し、releaseは失効させます。非空値は他modeで禁止です。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.allowEmptyAfter` | onprem-l2専用の正の期間です。観測開始後この期間を経て端末0件を完了扱いにでき、省略時は保留です。profileRef利用時は現行マージがこの項目のローカル上書きをコピーせず、プロファイル値が残ります。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources` | onprem-l2で必須の非空一覧で、provider-private-ipでは禁止です。type・resource・実効interface・network・bridgeの同一組み合わせは拒否します。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].type` | 必須のdhcpv4-lease・arp-observer・on-demand-arp・pve-svnetです。ARP・PVEはローカル観測デーモンを生成し、DHCPは実リースイベントを使います。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].resource` | 任意の参照元識別文字列で、既定空です。重複・観測識別fingerprintに使い、観測プロセス名や存在検証付きKind/name参照ではありません。イベント照合は主にtype・interface・network・bridgeです。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].interface` | 任意の観測インターフェースで、既定capture.interfaceです。on-demand-arpでは補完後に必須で、利用側が宣言別名をOS名へ解決します。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].bridge` | PVE・svnet観測照合に使う任意のローカルbridgeラベルです。既定空はbridge制限なしで、Bridge作成ではありません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].network` | svnet1等の任意のPVE・svnet論理networkラベルです。空ならnetwork選択条件なしで、IP CIDRではありません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom` | アクティブARP送信元の任意状態参照です。欠落・利用不能なら検証済み取得送信元へ戻り、どちらも解決しなければon-demand probeを生成しません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].scanInterval` | 任意の観測poll間隔で、明示値1秒以上、生成ARP・PVEデーモンの既定10秒です。イベント駆動DHCPではpollしません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeTimeout` | アクティブARP試行間の正のGo durationで、観測デーモン既定1秒です。on-demand probingで使います。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeRetries` | 0～20の追加ARP再試行数で、既定0です。総試行回数ではなく初回1回は別です。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].leaseTTL` | 任意の正のイベント寿命で、このsourceのownershipDiscovery.leaseTTLを上書きします。省略時は探索リース（既定5分）を継承します。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope` | プール内の任意の所有権観測アドレス制限です。外部prefixを任意に広告する認可ではありません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.includeAddresses` | プール内IPv4アドレス・CIDR許可一覧です。空なら全候補を許可し、有効シャード割当はプロバイダー探索のこの一覧を置き換えます。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.excludeAddresses` | 任意のプール内IPv4アドレス・CIDR除外一覧で、既定なしです。許可一覧の照合後に除外します。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector` | 任意のプロバイダー探索タグ条件です。クラウドのタグ設定を変更しません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector.tags` | 空でないタグキーと期待文字列のmapです。前後空白除去後の値が全キーで一致する必要があり、空mapなら制限なしです。プロバイダー探索用でonpremイベント照合には使いません。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.resource` | 状態参照利用時に必須の既存Kind/nameです。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.field` | resource利用時必須の参照元提供フィールドです。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.optional` | 既定falseです。参照検証を省略せず、未解決の送信元アドレスも補完しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller.go) · [pkg/controller/mobility/controller_effects.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller_effects.go) · [pkg/controller/mobility/bgp_delivery_planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/bgp_delivery_planner.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go) · [pkg/controller/mobility/discovery_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_support.go) · [pkg/controller/mobility/discovery_facts.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_facts.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/pool_plan.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/pool_plan.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/chain/sam.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam.go) · [pkg/controller/chain/sam_freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam_freebsd.go) · [cmd/routerd-arp-observer/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-arp-observer/main.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
