# tools/verify, built from the same source and vendored modules as the
# package, so the checks never download anything.
{
  buildGo127Module,
  xunhen,
}:

buildGo127Module {
  pname = "xunhen-verify";
  inherit (xunhen) version src vendorHash;
  subPackages = [ "tools/verify" ];
  env.CGO_ENABLED = "0";
  env.GOTOOLCHAIN = "local";
  doCheck = false;
  meta.mainProgram = "verify";
}
