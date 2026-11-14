# SAMEnrollmentPolicy

[リソース別の設定](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

RR側でSAM leafの識別・トンネルアドレス・任意のjoin認証・所有許可を規定します。leafはこのポリシーを複製せず、許可されたトポロジーを取得します。

## 指定条件と参照関係

- RR出力名はポリシーごとに一意にし、rrSetRef・rrNodeSetRefは組で指定します。directMeshにはpair-stableのトンネル割当とRR代替経路も必要です。
- 同じポリシー内のleaf ID・トンネルアドレス・非空WireGuard鍵・所有アドレス・非空BGP router IDの衝突を確認します。終端・WireGuard追加prefix・移動所有権は別々に認可します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    [ allowedLeafIDs:
        [ pattern: <string> ]
    ]
    [ directMesh:
        [ peerGroupRef: <string> ]
    ]
    [ endpointPrefixes:
        - <string>
    ]
    [ joinAudience: <string> ]
    [ joinTokenFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    [ mobilityPoolRefs:
        - <string>
    ]
    [ mobilityPrefixes:
        - <string>
    ]
    [ rrNodeSetRef: <string> ]
    [ rrSetRef: <string> ]
    transportProfileRef: <string>
    [ ttl: <string> ]
    tunnelAddressPrefixes:
      - <string>
    [ wireGuard:
        [ allowedExtraIPPrefixes:
            - <string>
        ]
        [ endpointPrefixes:
            - <string>
        ]
        [ interface: <string> ]
        [ persistentKeepalive: <integer> ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.transportProfileRef` | **必須** | string | 同じRouterに存在するSAMTransportProfile/nameが必須です。登録leafをそのプロファイルへ結び付ける参照で、状態値参照ではありません。 |
| `spec.rrSetRef` | **条件付き必須** | string | 任意の実行時出力名SAMRRSet/nameです。rrNodeSetRefと同時指定し、ポリシー間で一意にします。静的に記述するSAMRRSetではなく生成スナップショット名です。direct mesh・leaf側の取得では必要です。 |
| `spec.rrNodeSetRef` | **条件付き必須** | string | RR識別情報を供給する既存SAMNodeSet/nameです。rrSetRefと同時指定が必要で、全ノードがrouteReflector:trueである必要があります。ポリシー内に別のRR一覧は持ちません。 |
| `spec.joinTokenFrom` | *任意* | object | 任意のHMAC共通鍵参照です。省略時はjoin HMACを要求せず、保護されたControlAPIが信頼境界になります。指定時はfile・envのどちらか一方と、要求側のnonce・timestamp・HMACが必要です。ControlAPIのBearer認証とは別です。 |
| `spec.joinTokenFrom.file` | **条件付き必須** | string | ルーター上の生HMAC鍵ファイルでenvと排他です。前後空白を除き、オフライン検証で読めなくても実際の登録時には鍵欠落で失敗します。 |
| `spec.joinTokenFrom.env` | **条件付き必須** | string | HMAC鍵を持つrouterdプロセス環境変数名です。fileと排他で、環境ファイル自動読込はありません。 |
| `spec.joinTokenFrom.base64` | *任意* | boolean | 既定falseです。trueなら前後空白除去後の値を標準Base64デコードし、復号したバイト列をHMAC鍵に使います。Base64は暗号化ではありません。 |
| `spec.joinAudience` | *任意* | string | joinTokenFrom利用時の任意の要求audienceです。非空ならclaim.joinAudienceとの一致が必要で、既定audienceはありません。 |
| `spec.allowedLeafIDs` | *任意* | object | 任意のleaf識別子制限です。省略時は正規表現による制限なしです。 |
| `spec.allowedLeafIDs.pattern` | *任意* | string | 前後空白除去後のleafIDに照合するGo正規表現です。空なら他条件を満たす任意IDを許可し、自動で先頭末尾を固定しないため完全一致には^...$を使います。 |
| `spec.tunnelAddressPrefixes` | **必須** | array | 要求のtunnelAddress /32を許可する必須の非空IPv4 CIDR一覧です。この項目によるアドレス自動割当はありません。 |
| `spec.endpointPrefixes` | *任意* | array | 任意の外側終端IPv4 CIDR許可一覧です。空なら終端prefix制限なしです。wireGuard.endpointPrefixesが空のときはWireGuard終端の許可一覧にも使います。 |
| `spec.wireGuard` | *任意* | object | 任意の登録leaf WireGuard制御です。実動作のWireGuard設定を使う場合はinterfaceが必要で、終端prefix制限だけでデバイスは作成しません。 |
| `spec.wireGuard.interface` | **条件付き必須** | string | 既存WireGuardInterfaceのmetadata.nameです。Kind/nameではありません。どのローカルWireGuardが本ポリシーを取り込むかを制限し、自動作成は行いません。 |
| `spec.wireGuard.endpointPrefixes` | *任意* | array | 要求のWireGuard外側終端IPを許可する任意IPv4 CIDR一覧です。空ならendpointPrefixesを使い、両方空ならこのprefix制限はありません。 |
| `spec.wireGuard.allowedExtraIPPrefixes` | *任意* | array | claim.wireGuard.allowedIPsの追加分を許可する任意IPv4 CIDR一覧です。トンネル/32は別途生成ピアに加え、空なら追加prefixを許可しません。 |
| `spec.wireGuard.persistentKeepalive` | *任意* | integer; minimum=0; maximum=65535 | 0～65535秒で、既定0です。要求のkeepaliveが0の場合の生成WireGuardピア既定値で、要求の非0値を優先します。 |
| `spec.directMesh` | *任意* | object | 任意のポリシー単位direct leaf mesh出力です。RRの代替経路を除く指定ではありません。 |
| `spec.directMesh.peerGroupRef` | *任意* | string | SAMPeerGroup/nameの実行時出力です。空ならdirect mesh無効で、rrSetRefと参照SAMTransportProfileのaddressingMode: pair-stableが必要です。ポリシー間で一意にし、静的SAMPeerGroupではありません。 |
| `spec.mobilityPoolRefs` | *任意* | array | 所有権prefixの許可元となる既存MobilityPool/name一覧です。mobilityPrefixesと合成し、プール・クラウド資源作成の指定ではありません。 |
| `spec.mobilityPrefixes` | *任意* | array | 要求する所有IPv4 /32の追加許可CIDRです。参照プールと合成し、プール参照も空なら所有アドレスを許可しません。 |
| `spec.ttl` | *任意* | string | 任意のGo durationです。省略時の実登録は既定5分リースです。指定TTLはjoinTimestampからの要求寿命と明示expiresAtも制限します。形の検証は構文のみですが、不正・非正の寿命は実行時に失効扱いになります。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `acceptedClaims`, `skippedClaims`, `leafIDs`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
