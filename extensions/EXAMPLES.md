# Examples and original authors

[Extension guide](README.md) - [API reference](API.md)

These are the extension implementations currently available in the repository. They are examples of GoWarp ports, not a guarantee that every block in each original extension is supported. Check each module's block registrations before using it in a project.

## Extension catalog

Each **JavaScript** link opens a complete extension file with an embedded Go implementation. Download the raw file to load it into TurboWarp, or use its raw URL. The **Go module** links point to the corresponding test implementations for reference. Example files are maintained alongside the English documentation in `extensions/examples`.

| Extension | Original authors / credited contributors | GoWarp example / implementation |
| --- | --- | --- |
| Window API (`gowarpWindows`) | wheedo07 | [JavaScript](examples/gowarpWindows.js) · [Go module](../test/extensions/gowarpWindows/module.go) · [SB3 demo](../test/fixtures/projects/native-windows.sb3) |
| Delta Time (`dtbyxeroname`) | [XeroName](https://scratch.mit.edu/users/plant2014/) | [JavaScript](examples/Deltatime.js) · [Go module](../test/extensions/dtbyxeroname/module.go) |
| Camera V1 (Buggy) (`DTcameracontrols`) | DT | [JavaScript](examples/DTcameracontrols.js) · [Go module](../test/extensions/DTcameracontrols/module.go) |
| Shaded (`OACShaded`) | [ObviousAlexC](https://scratch.mit.edu/users/pinksheep2917/) | [JavaScript](examples/OACShaded.js) · [Go module](../test/extensions/OACShaded/module.go) |
| My Blocks+ (`SPmbpCST`) | SharkPool, [CST1229](https://scratch.mit.edu/users/CST1229/), [0znzw](https://scratch.mit.edu/users/0znzw/) | [JavaScript](examples/SPmbpCST.js) · [Go module](../test/extensions/SPmbpCST/module.go) |
| Renderer Control (`SPrenderControl`) | SharkPool | [JavaScript](examples/SPrenderControl.js) · [Go module](../test/extensions/SPrenderControl/module.go) |
| Sprite Effects (`SPspriteEffects`) | SharkPool | [JavaScript](examples/SPspriteEffects.js) · [Go module](../test/extensions/SPspriteEffects/module.go) |
| Sprite Effects V2 (`SPspriteEffectsV2`) | SharkPool | [JavaScript](examples/SPspriteEffectsV2.js) · [Go module](../test/extensions/SPspriteEffectsV2/module.go) |
| Tune Shark V3 (`SPtuneShark3`) | SharkPool | [JavaScript](examples/SPtuneShark3.js) · [Go module](../test/extensions/SPtuneShark3/module.go) |
| `ShovelUtils` | TheShovel, [Mio](https://scratch.mit.edu/users/0znzw/) | [JavaScript](examples/ShovelUtils.js) · [Go module](../test/extensions/ShovelUtils/module.go) |
| Window Controls (`cubesterWindowControls`) | [CubesterYT](https://scratch.mit.edu/users/CubesterYT/), BlueDome77 (original implementation) | [JavaScript](examples/cubesterWindowControls.js) · [Go module](../test/extensions/cubesterWindowControls/module.go) |
| Rich Presence (`cubesterRichPresence`) | [CubesterYT](https://scratch.mit.edu/users/CubesterYT/) | [JavaScript](examples/cubesterRichPresence.js) · [Go module](../test/extensions/discord/module.go) |
| Ping Cloud Data (`clouddataping`) | GarboMuffin, TheShovel | [JavaScript](examples/clouddataping.js) · [Go module](../test/extensions/clouddataping/module.go) |
| List Tools (`lmsData`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsData.js) · [Go module](../test/extensions/lmsData/module.go) |
| Keys+ V2 (`enderKeysPlusV2`) | StackOverflow | [JavaScript](examples/enderKeysPlusV2.js) · [Go module](../test/extensions/enderKeysPlusV2/module.go) |
| Files (`files`) | GarboMuffin | [JavaScript](examples/files.js) · [Go module](../test/extensions/files/module.go) |
| Tween (`jeremygamerTweening`) | [JeremyGamer13](https://scratch.mit.edu/users/JeremyGamer13/), [Fath11](https://scratch.mit.edu/users/Fath11/) | [JavaScript](examples/jeremygamerTweening.js) · [Go module](../test/extensions/jeremygamerTweening/module.go) |
| Asset Manager (`lmsAssets`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/), [Mio](https://scratch.mit.edu/users/0znzw/) | [JavaScript](examples/lmsAssets.js) · [Go module](../test/extensions/lmsAssets/module.go) |
| Looks Plus (`lmsLooksPlus`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsLooksPlus.js) · [Go module](../test/extensions/lmsLooksPlus/module.go) |
| More Events (`lmsMoreEvents`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsMoreEvents.js) · [Go module](../test/extensions/lmsMoreEvents/module.go) |
| Sound Expanded (`lmsSoundExpanded`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsSoundExpanded.js) · [Go module](../test/extensions/lmsSoundExpanded/module.go) |
| More Timers (`lmsTimers`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsTimers.js) · [Go module](../test/extensions/lmsTimers/module.go) |
| Clones Plus (`lmsclonesplus`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsclonesplus.js) · [Go module](../test/extensions/lmsclonesplus/module.go) |
| Comment Blocks (`lmscomments`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmscomments.js) · [Go module](../test/extensions/lmscomments/module.go) |
| Lily's Toolbox (`lmsutilsblocks`) | [LilyMakesThings](https://scratch.mit.edu/users/LilyMakesThings/) | [JavaScript](examples/lmsutilsblocks.js) · [Go module](../test/extensions/lmsutilsblocks/module.go) |
| Navigator (`navigatorinfo`) | GarboMuffin | [JavaScript](examples/navigatorinfo.js) · [Go module](../test/extensions/navigatorinfo/module.go) |
| More Motion (`nkmoremotion`) | [NamelessCat](https://scratch.mit.edu/users/NamelessCat/) | [JavaScript](examples/nkmoremotion.js) · [Go module](../test/extensions/nkmoremotion/module.go) |
| URL Playback (`notSound`) | [softed](https://scratch.mit.edu/users/softed/), GarboMuffin | [JavaScript](examples/notSound.js) · [Go module](../test/extensions/notSound/module.go) |
| Customizable Anti Aliasing (`oaccaa`) | [ObviousAlexC](https://scratch.mit.edu/users/pinksheep2917/) | [JavaScript](examples/oaccaa.js) · [Go module](../test/extensions/oaccaa/module.go) |
| Pen Plus V7 (`penP`) | [ObviousAlexC](https://scratch.mit.edu/users/pinksheep2917/), Pen-Group | [JavaScript](examples/penP.js) · [Go module](../test/extensions/penP/module.go) |
| Runtime Options (`runtimeoptions`) | GarboMuffin | [JavaScript](examples/runtimeoptions.js) · [Go module](../test/extensions/runtimeoptions/module.go) |
| Time (`sipctime`) | -SIPC-, [SharkPool](https://scratch.mit.edu/users/DemonX5/) | [JavaScript](examples/sipctime.js) · [Go module](../test/extensions/sipctime/module.go) |
| JSON (`skyhigh173JSON`) | [Skyhigh173](https://scratch.mit.edu/users/Skyhigh173/), [Mio](https://scratch.mit.edu/users/0znzw/) | [JavaScript](examples/skyhigh173JSON.js) · [Go module](../test/extensions/skyhigh173JSON/module.go) |
| Stretch (`stretch`) | GarboMuffin, TheStarWorld | [JavaScript](examples/stretch.js) · [Go module](../test/extensions/stretch/module.go) |
| Text (`strings`) | [CST1229](https://scratch.mit.edu/users/CST1229/), [BludIsAnLemon](https://scratch.mit.edu/users/BludIsAnLemon/), [Man-o-Valor](https://scratch.mit.edu/users/man-o-valor/) | [JavaScript](examples/strings.js) · [Go module](../test/extensions/strings/module.go) |
| Animated Text (`text`) | LilyMakesThings, GarboMuffin | [JavaScript](examples/text.js) · [Go module](../test/extensions/text/module.go) |
| Canvas Effects (`theshovelcanvaseffects`) | TheShovel, [SharkPool](https://scratch.mit.edu/users/DemonX5/) | [JavaScript](examples/theshovelcanvaseffects.js) · [Go module](../test/extensions/theshovelcanvaseffects/module.go) |
| Math (`truefantommath`) | [TrueFantom](https://scratch.mit.edu/users/TrueFantom/) | [JavaScript](examples/truefantommath.js) · [Go module](../test/extensions/truefantommath/module.go) |
| Clipping & Blending (`xeltallivclipblend`) | [Vadik1](https://scratch.mit.edu/users/Vadik1/) | [JavaScript](examples/xeltallivclipblend.js) · [Go module](../test/extensions/xeltallivclipblend/module.go) |
| Camera V2 (`SPcamera`) | SharkPool | [JavaScript](examples/SPcamera.js) · [Go module](../test/extensions/SPcamera/module.go) |
| Looks Expanded (`SPlooksExpanded`) | SharkPool | [JavaScript](examples/SPlooksExpanded.js) · [Go module](../test/extensions/SPlooksExpanded/module.go) |
| Graphics2D (`nonameawagraph`) | NOname-awa | [JavaScript](examples/nonameawagraph.js) · [Go module](../test/extensions/nonameawagraph/module.go) |

## Attribution and corrections

GoWarp adaptations are maintained as part of GoWarp. Original extension authors retain credit for their work. Keep original author and license notices when distributing an adapted extension; the root GoWarp license does not replace third-party license notices.

To correct a credit or add a missing author, provide the original extension source or an author attribution link. If adding an example, include its extension ID, a working implementation link, original author credits, and any original license notice.