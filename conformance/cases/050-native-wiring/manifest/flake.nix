{
  description = "consumer";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # keep this output
  outputs = { self, nixpkgs }: {};
}
