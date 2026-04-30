{ config, lib, pkgs, ... }:

let
  cfg = config.services.routerd;
  configFile =
    if cfg.configFile != null then cfg.configFile
    else pkgs.writeText "router.yaml" cfg.configText;
in {
  options.services.routerd = {
    enable = lib.mkEnableOption "routerd declarative router applier";

    package = lib.mkOption {
      type = lib.types.package;
      description = "routerd package to use.";
    };

    configFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      description = ''
        Path to a router.yaml. If null, configText must be provided
        and is materialized into the Nix store.
      '';
    };

    configText = lib.mkOption {
      type = lib.types.nullOr lib.types.lines;
      default = null;
      description = "Inline router.yaml. Ignored if configFile is set.";
    };

    socket = lib.mkOption {
      type = lib.types.str;
      default = "/run/routerd/routerd.sock";
      description = "Control API Unix socket path.";
    };

    applyInterval = lib.mkOption {
      type = lib.types.str;
      default = "60s";
      description = "Periodic apply interval, as a Go duration.";
    };

    extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Additional command-line flags passed to routerd serve.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.configFile != null || cfg.configText != null;
        message = "services.routerd: set either configFile or configText.";
      }
    ];

    environment.systemPackages = [ cfg.package ];

    # routerd is a privileged router controller. The applier currently
    # expects to be able to call ip, sysctl, systemctl, dnsmasq, nft, and
    # related tools, so the unit runs as root for now. Hardening will
    # tighten the unit once each renderer reports its required
    # capabilities.
    systemd.services.routerd = {
      description = "routerd declarative router controller";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-pre.target" ];
      wants = [ "network-pre.target" ];

      path = with pkgs; [
        iproute2
        nftables
        dnsmasq
        conntrack-tools
        ppp
        dnsutils
        iputils
        tcpdump
        traceroute
        systemd
      ];

      serviceConfig = {
        Type = "simple";
        ExecStart = lib.concatStringsSep " " ([
          "${cfg.package}/bin/routerd"
          "serve"
          "--config" "${configFile}"
          "--socket" cfg.socket
          "--apply-interval" cfg.applyInterval
        ] ++ cfg.extraFlags);
        Restart = "always";
        RestartSec = "2s";
        RuntimeDirectory = "routerd";
        StateDirectory = "routerd";
      };
    };
  };
}
