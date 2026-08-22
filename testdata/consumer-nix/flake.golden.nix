{
  description = "consumer";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # keep this output
  outputs = { self, nixpkgs }: {};
  # git-a2a:begin acme-lib-utils
  inputs.acme-lib-utils.url = "git+https://github.com/acme/lib-utils.git?dir=nix%2Flib&ref=main&rev=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  # git-a2a:end acme-lib-utils
}
