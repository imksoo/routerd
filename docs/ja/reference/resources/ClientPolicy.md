# ClientPolicy

[リソース別の設定](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 任意; array
      - # object
        ipv4Reservation: "<string>" # 任意; string
        ipv6Addresses: # 任意; array
          - "<string>" # string
        match: # 必須; object
          dhcpFingerprints: # 任意; array
            - "<string>" # string
          hostnamePatterns: # 任意; array
            - "<string>" # string
          macs: # 任意; array
            - "<string>" # string
          ouiPrefixes: # 任意; array
            - "<string>" # string
        mode: "trusted" # 必須; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 任意; string
    guestEgressAllow: # 任意; array
      - "<string>" # string
    guestEgressDeny: # 任意; array
      - "<string>" # string
    guestServices: # 任意; array
      - "<string>" # string
    interfaces: # 任意; array
      - "<string>" # string
    isolation: # 任意; object
      lanInternet: "" # 任意; string; enum=["", "allow", "deny"]
      lanLAN: "" # 任意; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 任意; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 任意; string; enum=["", "allow", "deny"]
    macs: # 任意; array
      - "<string>" # string
    mode: "include" # 必須; string; enum=["include", "exclude"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
