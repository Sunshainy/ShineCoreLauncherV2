const fs = require("fs");
const path = require("path");
const pngToIco = require("png-to-ico");

const root = process.cwd();
const src = path.join(root, "frontend", "src", "assets", "images", "icon.png");
const outApp = path.join(root, "build", "appicon.ico");
const outWin = path.join(root, "build", "windows", "icon.ico");

pngToIco(src)
  .then((buf) => {
    fs.writeFileSync(outApp, buf);
    fs.writeFileSync(outWin, buf);
    console.log("ICO generated:", outApp, outWin);
  })
  .catch((err) => {
    console.error("ICO generation failed:", err);
    process.exit(1);
  });
