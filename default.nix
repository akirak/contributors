{ src, runCommandLocal, makeWrapper, buildGoPackage, github-linguist }:
let
  package = buildGoPackage {
    name = "contributors";
    goPackagePath = "akirak/contributors";
    inherit src;
    # Run vgo2nix to update deps.nix
    goDeps = ./deps.nix;
  };
in
runCommandLocal "contributors" {
  propagatedBuildInputs = [
    package
    github-linguist
  ];

  nativeBuildInputs = [
    makeWrapper
  ];
} ''
  mkdir -p $out/bin
  install ${package}/bin/contributors $out/bin/contributors
  wrapProgram $out/bin/contributors \
    --prefix PATH : ${github-linguist}/bin
''
