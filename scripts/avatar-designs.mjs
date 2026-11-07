// Polymux characters and DS Bot redesigns. Coordinates are x right, y up, z forward.
import { rounded } from './avatar-curves.mjs'
export function redesign(models, tools) {
  const { ellipsoid: oval, sphere, capsule, roundCone, roundBox: box, extrude, polygonDistance, torus, smin } = tools
  const union = (...parts) => p => Math.min(...parts.map(f => f(p)))
  const profile = points => {
    const outline = rounded(points, .22)
    return (x, y) => polygonDistance([outline], x, y)
  }
  const slab = (points, depth = .25, radius = .06) => extrude(profile(points), depth, radius)
  const lathe = (points, round = .025) => p => polygonDistance([points], Math.hypot(p[0],p[2]), p[1])-round
  const paint = (fill, region, wrap = false) => ({ fill, region, wrap })
  const band = (fill, test) => paint(fill, p => test(...p) ? -1 : 1, true)
  const spot = (fill, x, y, rx, ry) => paint(fill, p => Math.hypot((p[0]-x)/rx, (p[1]-y)/ry)-1)
  const set = (id, sdf, eyes, paints, eye = .8) => { models[id] = { sdf, eyes, paints, eye } }

  // A round whale: a big head, an upswept tail with a broad fluke, a little spout and a grooved pale throat.
  const fountain = union(capsule([.3,.6,0],[.3,.84,0],.05),sphere([.3,.95,0],.08),
    oval([.15,.92,0],[.14,.065,.065],-35),oval([.45,.92,0],[.14,.065,.065],35))
  const whale = smin(.12,oval([.18,0,0],[.82,.66,.6]),roundCone([-.35,-.06,0],[-.9,.22,0],.36,.12),
    oval([-1,.42,0],[.12,.27,.09],24),oval([-1.12,.16,0],[.12,.27,.09],104),
    oval([.12,-.4,.5],[.22,.09,.08],-30))
  set('whale', union(whale,fountain), [[.12,.12],[.4,.12]], [
    band('#B9E4F3',(x,y,z) => y < -.22 && x > -.45),
    paint('#A6DCF5',p => fountain(p)-whale(p),true),
    spot('#F4A3C2',.7,-.04,.08,.05)],.68)
  // Horns, ears, a raised muzzle, and two nostrils rather than a single brown blob.
  set('zed', union(oval([0,-.1,0],[.71,.72,.5]),
    oval([-.73,.3,0],[.34,.17,.16],-20), oval([.73,.3,0],[.34,.17,.16],20),
    capsule([-.5,.45,0],[-.8,.73,0],.14), capsule([-.8,.73,0],[-.77,1.02,0],.1),
    capsule([.5,.45,0],[.8,.73,0],.14), capsule([.8,.73,0],[.77,1.02,0],.1),
    oval([0,-.42,.39],[.49,.28,.29])), [[-.23,.12],[.23,.12]], [
    band('#F3D9AB',(x,y) => y > .53 && Math.abs(x) > .55),
    band('#DDAF91',(x,y,z) => y < -.2 && z > .4),
    spot('#62473F',-.19,-.42,.07,.055), spot('#62473F',.19,-.42,.07,.055),
    spot('#DDAF91',-.82,.3,.16,.075), spot('#DDAF91',.82,.3,.16,.075),
  ])
  // A real crescent, with its face on the thick side and a few broad crater patches.
  set('moon', extrude((x,y) => Math.max(Math.hypot(x,y)-.94, .86-Math.hypot(x-.48,y-.2)),.24,.045),
    [[-.72,-.08],[-.5,-.08]], [spot('#D99A38',-.55,-.48,.14,.13),
      spot('#F6D980',-.57,.48,.13,.15), spot('#D99A38',-.12,-.72,.09,.07)], .52)
  const wave = slab([[-1,-.64],[-.86,-.15],[-.65,.46],[-.25,.9],[.25,1],[.64,.8],
    [.74,.48],[.5,.37],[.45,.63],[.18,.68],[-.08,.5],[-.12,.16],[.08,-.12],
    [.5,-.29],[.99,-.39],[1.03,-.72],[.45,-.88],[-.38,-.88]],.25,.04)
  const foam = union(...[[-.25,.87],[.05,.95],[.32,.91],[.55,.75],[.62,.53]]
    .map(([x,y])=>sphere([x,y,.02],.13)))
  set('wave',union(wave,foam), [[-.57,-.18],[-.25,-.18]], [
    band('#2E7EAA',(x,y)=>y<-.61),
    band('#8DDADC',(x,y,z)=>z>.18&&x>-.12&&y<-.2&&y>-.51+.12*Math.sin(x*3)),
    paint('#E0F4EA',p=>Math.min(foam(p)-wave(p),.69-p[1]),true)],.72)
  const petals = Array.from({length:5},(_,i) => {
    const a = Math.PI/2+i*Math.PI*2/5
    return oval([Math.cos(a)*.57,Math.sin(a)*.57,0],[.38,.54,.23],a*180/Math.PI-90)
  })
  const flowerCenter = oval([0,0,.2],[.43,.43,.22])
  set('hex',union(...petals,flowerCenter),[[-.16,.04],[.16,.04]],[
    band('#F4CF68',(...p)=>flowerCenter(p)<Math.min(...petals.map(petal=>petal(p))))],.65)
  const bucket = lathe([[0,-.83],[.53,-.83],[.75,.25],[.78,.25],[.78,.32],[0,.32]],.035)
  const kernels = [...Array.from({length:8},(_,i)=>[Math.cos(i*Math.PI/4)*.57,.43,Math.sin(i*Math.PI/4)*.57]),
    ...Array.from({length:5},(_,i)=>[Math.cos(i*Math.PI*2/5)*.3,.66,Math.sin(i*Math.PI*2/5)*.3]),[0,.79,0]]
  const cornBase = union(...kernels.map(p=>sphere(p,.17)),oval([0,.4,0],[.62,.25,.62]))
  const cornLobes = union(...kernels.flatMap(([x,y,z],i)=>Array.from({length:3},(_,k)=>{
    const a=k*Math.PI*2/3+i*.7
    return sphere([x+Math.cos(a)*.105,y+.05+(k===0?.045:0),z+Math.sin(a)*.105],.14)
  })))
  const corn = smin(.035,cornBase,cornLobes)
  set('popcorn',union(bucket,corn),[[-.21,-.18],[.21,-.18]],[
    ...Array.from({length:7},(_,i)=>{
      const a=i*Math.PI*2/7
      return band('#CE5750',(x,y,z)=>bucket([x,y,z])<corn([x,y,z])&&
        Math.cos(Math.atan2(x,z)-a)>.96)
    }),
    band('#F7E9CB',(x,y,z)=>z>.4&&Math.hypot(x/.38,(y+.18)/.31)<1),
    band('#CE5750',(x,y,z)=>(y>.22||y<-.77)&&bucket([x,y,z])<corn([x,y,z])),
    band('#E5C783',(x,y,z)=>corn([x,y,z])<bucket([x,y,z])),
    band('#FFF0C2',(x,y,z)=>cornLobes([x,y,z])<Math.min(cornBase([x,y,z]),bucket([x,y,z])))],.62)
  set('peanut', smin(.12,oval([0,.43,0],[.54,.62,.43]),oval([0,-.48,0],[.62,.67,.46])),
    [[-.19,.43],[.19,.43]], [
      band('#AB763F',(x,y,z) => Math.abs(x) < .035),
      paint('#DCB47D',p => Math.abs(Math.sin((p[0]+p[1])*12)) < .22 || Math.abs(Math.sin((p[0]-p[1])*12)) < .22 ? -1 : 1),
    ], .72)
  // A big glass dome carrying the face, on a disc ringed with lights that show from every side.
  const disc = oval([0,-.36,0],[1.06,.21,.78])
  const lights = Array.from({length:8},(_,i) => [Math.cos(i*Math.PI/4)*1.02,-.36,Math.sin(i*Math.PI/4)*.74])
  set('saucer', union(disc,p => Math.max(sphere([0,-.2,0],.62)(p),-.3-p[1])), [[-.17,-.06],[.17,-.06]], [
    paint('#79CFCF',p => disc(p) < .02 ? 1 : -1, true), band('#7C8DA6',(x,y) => y < -.44),
    paint('#F7D66F',p => Math.min(...lights.map(c => Math.hypot(p[0]-c[0],p[1]-c[1],p[2]-c[2])))-.075, true),
  ], .72)
  // A round lighthouse: plinth, striped tower, gallery deck, lantern and cone roof.
  set('beacon', union(lathe([[0,-.98],[.62,-.98],[.62,-.78],[.48,-.78],[.36,.28],[.47,.28],[.47,.4],
    [.3,.4],[.3,.68],[.44,.68],[0,.98]],.04),sphere([0,1,0],.09)),
    [[-.12,-.28],[.12,-.28]], [band('#F0E5CF',(x,y) => (y > -.62 && y < -.46) || (y > -.02 && y < .14)),
      band('#F3CD66',(x,y) => y > .43 && y < .66), band('#386B61',(x,y) => y > .66 || (y > .26 && y <= .43) || y < -.76)], .58)
  // Plus is a plain soft cross, so it takes the chosen colour like the other shapes.
  set('plus', models.plus.sdf, [[-.14,.02],[.14,.02]], [], .72)
  set('magnet', extrude((x,y) => Math.max(y < 0 ? Math.abs(Math.hypot(x,y)-.58)-.23 : Math.abs(Math.abs(x)-.58)-.23,y-.82),.28,.06),
    [[-.17,-.57],[.17,-.57]], [band('#DCE5EB',(x,y) => y > .46),
      band('#9DAEBE',(x,y,z) => y > .46 && Math.abs(z) < .2),
      band('#B73C4C',(x,y,z) => y <= .46 && Math.abs(z) < .2)], .64)
  const cap = p => Math.max(oval([0,.36,0],[.84,.4,.7])(p),.18-p[1])
  set('acorn', union(oval([0,-.23,0],[.64,.79,.54]),cap,capsule([0,.65,0],[.12,.94,0],.085)),
    [[-.21,-.16],[.21,-.16]], [band('#79543D',(x,y) => y > .18),
      band('#9B7050',(x,y,z) => y > .24 && y < .65 && (Math.abs(Math.sin((x+y)*15)) < .25 || Math.abs(Math.sin((x-y)*15)) < .25)),
      band('#D8A569',(x,y,z) => y < -.5 && x < -.22 && z > .2)], .8)
  set('pebbles', union(oval([0,-.59,0],[.95,.32,.56]),oval([-.04,-.09,.01],[.73,.3,.47]),
    oval([.05,.42,.02],[.48,.31,.4])), [[-.08,.4],[.16,.4]], [
    band('#778D91',(x,y) => y < -.33),band('#B9C3B4',(x,y) => y > .19),
    spot('#DFE4D4',-.17,.58,.1,.055)], .5)
  const bulb = smin(.12,sphere([0,.3,0],.69),roundCone([0,-.08,0],[0,-.6,0],.47,.25))
  const socket = lathe([[0,-1],[.18,-1],[.26,-.88],[.29,-.82],[.29,-.53],[.25,-.47],[0,-.47]],.025)
  set('bulb',union(bulb,socket), [[-.23,.31],[.23,.31]], [
    band('#E2BB60',(x,y,z)=>y>-.47&&-.4*x+.5*(y-.25)+.7*z>0),
    band('#F0D38B',(x,y,z)=>y>-.47&&-.4*x+.5*(y-.25)+.7*z>.5),
    band('#81929C',(x,y)=>y<-.47),
    band('#C6D0CE',(x,y)=>y<-.52&&y>-.87&&Math.abs(Math.sin((y+.53)*32))<.35),
    band('#505E69',(x,y)=>y<-.9)],.72)
  // A cup trophy: a rounded hollow bowl, an open handle each side, a knotted stem and a plinth with a plaque.
  const hollow = [[0,.05],[.2,.07],[.35,.16],[.43,.3],[.48,.5],[.5,.8]]
  const bowl = lathe([[0,-.18],[.2,-.17],[.35,-.09],[.46,.06],[.54,.25],[.59,.48],[.61,.8],
    ...hollow.slice().reverse()],.025)
  const inside = (x,y,z) => polygonDistance([[...hollow,[.5,.9],[0,.9]]],Math.hypot(x,z),y) < 0
  const stem = lathe([[0,-.62],[.16,-.62],[.1,-.5],[.08,-.37],[.14,-.31],[.14,-.25],[.1,-.21],[0,-.21]],.02)
  set('trophy', smin(.04,bowl,stem,box([0,-.64,0],[.33,.05,.27],.03),box([0,-.8,0],[.48,.12,.4],.05),
    torus([.63,.46,0],.24,.085),torus([-.63,.46,0],.24,.085)), [[-.19,.33],[.19,.33]], [
    band('#8A6830',inside),
    band('#B98634',(x,y) => y < -.685),
    band('#F2D68E',(x,y,z) => z > .36 && Math.abs(x) < .25 && y < -.72 && y > -.88),
    band('#F2D488',(x,y,z) => z > .3 && x < -.27 && x > -.41 && y > .2 && y < .62 && !inside(x,y,z))], .8)
  const gemWidth = y => y>.08 ? 1.06-(y-.08)*.4/.44 : 1.06*(y+.7)/.78
  const gemDepth = y => y>.08 ? .5-(y-.08)*.23/.44 : .5*(y+.7)/.78
  const diamond = ([x,y,z]) => Math.max((Math.abs(x)-gemWidth(y))*.7,Math.abs(z)-gemDepth(y),
    (Math.abs(x)+Math.abs(z)*.8-gemWidth(y))*.7,y-.52,-.7-y)
  set('gem',diamond,[[-.24,.04],[.24,.04]],[
    band('#B9E5F1',(x,y)=>x<-gemWidth(y)*.42),
    band('#4284B4',(x,y)=>x>gemWidth(y)*.42),
    band('#A8DCEB',(x,y)=>y>.08),
    band('#E0F5FA',(x,y)=>y>.08&&Math.abs(x)<.48-(y-.08)*.2),
    band('#F0FAFC',(x,y)=>y>.50)],.62)
  const bunTop = p=>Math.max(oval([0,.15,0],[.88,.6,.68])(p),.1-p[1])
  const bunBase = oval([0,-.55,0],[.82,.22,.62])
  const patty = oval([0,-.34,0],[.84,.14,.64])
  const tomato = oval([0,-.12,0],[.84,.075,.65])
  const cheese = box([0,-.22,0],[.64,.035,.55],.05)
  const lettuce = ([x,y,z])=>Math.max(Math.abs(y-.005)-.07,
    Math.hypot(x,z*1.25)-.87-.035*Math.sin(7*Math.atan2(z,x)))
  const burgerParts = [bunTop,bunBase,patty,tomato,cheese,lettuce]
  const burgerNearest = part=>(...p)=>part(p)<=Math.min(...burgerParts.map(f=>f(p)))
  set('hamburger',smin(.015,...burgerParts),[[-.23,.36],[.23,.36]],[
    band('#6C4632',burgerNearest(patty)),band('#CA6752',burgerNearest(tomato)),
    band('#EDCA59',burgerNearest(cheese)),band('#70A35E',burgerNearest(lettuce)),
    ...[[-.44,.5],[-.18,.66],[.13,.64],[.43,.51]].map(([a,b])=>
      band('#F8E4B9',(x,y,z)=>z>.2&&burgerNearest(bunTop)(x,y,z)&&Math.hypot((x-a)/.028,(y-b)/.045)<1))],.65)
  set('carrot', smin(.07,roundCone([0,.32,0],[.05,-.88,0],.46,.065),
    capsule([0,.4,0],[-.34,.93,0],.12),capsule([0,.4,0],[0,1.1,0],.12),
    capsule([0,.4,0],[.35,.92,0],.12)), [[-.12,.03],[.12,.03]], [
    band('#55995A',(x,y) => y > .48),
    paint('#DB7031',p => p[1] < -.2 && Math.abs(Math.sin(p[1]*15)) < .14 && p[0] < .05 ? -1 : 1)], .58)
  set('pizza', slab([[-.85,.64],[.85,.64],[0,-.96]],.23,.09), [[-.19,.14],[.19,.14]], [
    band('#CA8649',(x,y) => y > .47),
    spot('#D76352',-.47,.27,.13,.13),spot('#D76352',.44,.28,.12,.12),spot('#D76352',.03,-.48,.12,.12),
    spot('#70985A',-.22,-.2,.045,.09),spot('#70985A',.28,-.07,.06,.045)], .72)
  set('sushi', union(box([0,-.25,0],[.83,.43,.5],.23),box([0,.25,0],[.89,.2,.55],.17)),
    [[-.3,-.17],[.3,-.17]], [band('#EE8C78',(x,y) => y > .1),
      band('#FAD3B7',(x,y,z) => y > .1 && Math.abs(Math.sin((x+z)*10)) < .19),
      band('#466A59',(x,y,z) => Math.abs(x) < .15)], .8)
  const cone = lathe([[0,-1.12],[.43,-.3],[.43,-.22],[0,-.22]])
  const vanilla = union(sphere([0,-.04,0],.54),
    capsule([-.31,-.3,.31],[-.32,-.49,.32],.105),
    capsule([.21,-.33,.36],[.2,-.58,.36],.09),
    sphere([.43,-.28,0],.14))
  const strawberry = union(sphere([-.06,.64,0],.5),
    capsule([-.34,.34,.24],[-.36,.13,.3],.105),
    capsule([.17,.3,.36],[.16,.04,.5],.1),sphere([.34,.31,0],.13))
  const scoop = union(vanilla,strawberry)
  set('iceCream', union(cone,scoop), [[-.2,.57],[.08,.57]], [
    paint('#FFF0CC',p => vanilla(p)-strawberry(p),true),
    paint('#E6AC6B',p => cone(p)-scoop(p),true),
    paint('#F6D39A',p => Math.max(cone(p)-scoop(p),
      Math.min(Math.abs(Math.sin((p[0]+p[1])*14)),Math.abs(Math.sin((p[0]-p[1])*14)))-.16),true),
    spot('#FCE3E5',-.31,.89,.1,.06)], .58)

  const sunFace = oval([0,0,.04],[.7,.7,.3])
  const sunRays = union(...Array.from({length:10},(_,i)=>{
    const a=i*Math.PI/5
    return roundCone([Math.cos(a)*.65,Math.sin(a)*.65,-.12],[Math.cos(a)*1.04,Math.sin(a)*1.04,-.12],.14,.025)
  }))
  set('sun',union(sunFace,sunRays),[[-.23,.06],[.23,.06]],[
    band('#EBA64E',(x,y)=>Math.hypot(x,y)>.7),
    spot('#EFA272',-.39,-.19,.09,.06),spot('#EFA272',.39,-.19,.09,.06)],.76)
  const leafBlade = ([x,y,z])=>Math.max(Math.hypot(x-.38,y,z*3.2)-1,Math.hypot(x+.38,y,z*3.2)-1)/3.2
  const leafStem = capsule([0,-.83,0],[0,-1.13,0],.045)
  set('leaf',union(leafBlade,leafStem),[[-.24,.13],[.24,.13]],[
    band('#4F9658',(x,y)=>x>0),
    band('#B8D585',(x,y,z)=>leafStem([x,y,z])<leafBlade([x,y,z])||z>0&&Math.abs(x)<.035),
    ...[-1,1].flatMap(s=>[
      band('#B8D585',(x,y,z)=>z>.1&&s*x>.035&&y<-.12&&y+.48-s*x*.65<.035),
      band(s<0?'#80B96F':'#4F9658',(x,y,z)=>z>.1&&s*x>.035&&y<-.12&&y+.48-s*x*.65<-.035)])],.75)
  set('tree', union(capsule([0,-.87,0],[0,.08,0],.2),sphere([0,.41,0],.63),
    sphere([-.45,.03,0],.43),sphere([.45,.03,0],.43)), [[-.23,.31],[.23,.31]], [
    band('#9C7150',(x,y) => y < -.35),band('#4B8E5E',(x,y) => y < .1 && Math.abs(x) > .25),
    spot('#9FCB7A',-.22,.71,.2,.1)], .8)
  set('cactus', smin(.1,capsule([0,-.77,0],[0,.65,0],.35),
    capsule([-.22,-.18,0],[-.67,-.18,0],.19),capsule([-.67,-.18,0],[-.67,.32,0],.19),
    capsule([.22,-.38,0],[.66,-.38,0],.19),capsule([.66,-.38,0],[.66,.08,0],.19)),
    [[-.1,.23],[.1,.23]], [
    band('#438D71',(x,y,z) => Math.abs(x) < .04),
    ...[[-.16,-.24],[.17,-.48],[-.67,.12],[.65,-.15]].map(([x,y]) => spot('#C8DB9C',x,y,.035,.075))], .48)
  set('mushroom', union(oval([0,-.43,0],[.4,.6,.34]),
    p => Math.max(oval([0,.28,0],[.92,.65,.55])(p),.05-p[1])), [[-.12,-.43],[.12,-.43]], [
    band('#F3DEB5',(x,y) => y < .09),spot('#F8E7C8',-.43,.37,.18,.14),
    spot('#F8E7C8',.34,.54,.16,.13),spot('#F8E7C8',.65,.19,.1,.08)], .58)
  set('mountain', slab([[-1,-.74],[0,1],[1,-.74]],.35,.08), [[-.23,-.3],[.23,-.3]], [
    band('#C4D7D5',(x,y) => x > .08),
    band('#F1F0DE',(x,y) => y > .35+Math.abs(Math.sin(x*9))*.13)], .8)
  set('volcano', lathe([[0,-.72],[.93,-.72],[.4,.61],[.23,.61],[.26,.4],[0,.38]],.07),
    [[-.24,-.29],[.24,-.29]], [band('#6F6370',(x,y) => x > .3),
      band('#F2964C',(x,y,z) => y > .45 || (y > -.02 && Math.abs(x-.12*Math.sin(y*13)) < .09)),
      spot('#B94D49',0,.6,.29,.045)], .78)
  const rainbow = (x,y) => Math.max(Math.abs(Math.hypot(x,y+.3)-.72)-.3,-.3-y)
  set('rainbow', extrude(rainbow,.2,.045), [[-.19,.53],[.19,.53]], [
    band('#F5C565',(x,y) => Math.hypot(x,y+.3) < .88),
    band('#8DC3AD',(x,y) => Math.hypot(x,y+.3) < .73),
    band('#8FAED6',(x,y) => Math.hypot(x,y+.3) < .58)], .61)
  const frost = []
  for(let i=0;i<6;i++) {
    const a=i*Math.PI/3+Math.PI/6, u=[Math.cos(a),Math.sin(a)]
    frost.push(capsule([0,0,0],[u[0],u[1],0],.09))
    for(const side of [-1,1]) {
      const b=a+side*.65
      frost.push(capsule([u[0]*.61,u[1]*.61,0],[u[0]*.61+Math.cos(b)*.29,u[1]*.61+Math.sin(b)*.29,0],.065))
    }
  }
  set('snowflake',union(sphere([0,0,0],.4),...frost), [[-.12,.02],[.12,.02]], [
    band('#87C7DD',(x,y) => Math.hypot(x,y) > .48)], .5)

  set('rocket', union(oval([0,.1,0],[.48,.94,.4]),
    slab([[-.28,-.25],[-.7,-.8],[-.64,-.96],[-.23,-.66]],.2,.05),
    slab([[.28,-.25],[.7,-.8],[.64,-.96],[.23,-.66]],.2,.05)), [[-.16,.15],[.16,.15]], [
    band('#D86862',(x,y) => y > .63 || (Math.abs(x) > .4 && y < -.3)),
    spot('#89CCDB',0,.17,.34,.36),band('#798DAB',(x,y) => y < -.65 && Math.abs(x) < .35)], .6)
  set('robot',union(box([0,-.05,0],[.73,.64,.6],.19),
    box([-.83,0,0],[.15,.25,.3],.08),box([.83,0,0],[.15,.25,.3],.08),
    capsule([0,.55,0],[0,.87,0],.06),sphere([0,.94,0],.13)), [[-.25,.11],[.25,.11]], [
    band('#788EAD',(x,y) => Math.abs(x) > .7 || y > .58),band('#F1B566',(x,y) => y > .82),
    band('#E2EEE8',(x,y,z) => z>.5 && Math.abs(x)<.29 && Math.abs(y+.34)<.085),
    ...[-.15,0,.15].map(line=>band('#788EAD',(x,y,z) => z>.5 && Math.abs(x-line)<.018 && Math.abs(y+.34)<.085))], .8)
  const cameraBody = union(box([0,-.08,0],[.9,.59,.34],.16),box([-.3,.57,0],[.28,.15,.24],.06))
  const barrel = p=>extrude((x,y)=>Math.hypot(x-.1,y+.1)-.44,.19,.035)([p[0],p[1],p[2]-.53])
  const lensRim = torus([.1,-.1,.76],.405,.06)
  const lensGlass = oval([.1,-.1,.77],[.355,.355,.075])
  const cameraParts = [cameraBody,barrel,lensRim,lensGlass]
  const cameraNearest = part=>(...p)=>part(p)<=Math.min(...cameraParts.map(f=>f(p)))
  set('camera',union(...cameraParts),[[-.055,-.06],[.255,-.06]],[
    band('#5B7288',(x,y,z)=>(y<-.38||y>.48)&&cameraNearest(cameraBody)(x,y,z)),
    band('#526478',cameraNearest(barrel)),band('#33475C',cameraNearest(lensRim)),
    band('#9CD5D6',cameraNearest(lensGlass)),
    band('#EBDBA7',(x,y,z)=>z>.4&&cameraNearest(cameraBody)(x,y,z)&&Math.hypot((x+.58)/.13,(y-.25)/.09)<1)],.59)
  set('clock',union(extrude((x,y)=>Math.hypot(x,y)-.78,.25,.09),
    oval([-.48,.75,0],[.3,.16,.2],35),oval([.48,.75,0],[.3,.16,.2],-35),
    capsule([-.43,-.62,0],[-.57,-.86,0],.08),capsule([.43,-.62,0],[.57,-.86,0],.08)),
    [[-.23,.2],[.23,.2]], [band('#E8B463',(x,y) => Math.hypot(x,y) > .65),
    paint('#8D7658',p => Math.abs(p[0]) < .035 && p[1] > -.34 && p[1] < -.05 || Math.abs(p[1]+.34) < .035 && p[0] > -.03 && p[0] < .28 ? -1 : 1)], .75)
  const keyHead=(x,y)=>Math.max(Math.hypot(x,y-.42)-.57,.17-Math.hypot(x,y-.65))
  set('key',union(extrude(keyHead,.19,.05),box([0,-.51,0],[.12,.55,.19],.045),
    box([.18,-.7,0],[.2,.09,.19],.03),box([.18,-.94,0],[.2,.09,.19],.03)),
    [[-.2,.25],[.2,.25]], [band('#D4A047',(x,y,z) => Math.abs(z) < .12),
      spot('#F8DB86',-.33,.54,.08,.13)], .65)
  const bell = lathe([[0,.57],[.23,.57],[.35,.44],[.4,.15],[.47,-.2],[.66,-.47],
    [.79,-.54],[.79,-.65],[.61,-.65],[.53,-.44],[.34,-.1],[.26,.27],[0,.35]])
  set('bell',union(bell,capsule([0,.36,0],[0,-.37,0],.055),
    sphere([0,-.44,0],.14),torus([0,.79,0],.16,.07)), [[-.13,-.03],[.13,-.03]], [
    band('#CF963D',(x,y,z) => y < -.49 && Math.hypot(x,z) > .25),
    band('#F8DB8B',(x,y,z) => x < -.24 && y > -.45 && y < .42 && z > .15)], .58)
  set('book',box([0,0,0],[.74,.94,.29],.16), [[-.24,.17],[.19,.17]], [
    band('#3F7D94',(x,y) => x < -.53),band('#F4E7CA',(x,y,z) => x > .63 || (y < -.78 && z < .17)),
    paint('#A4D6D5',p => Math.max(Math.abs(p[0]+.02)-.35,Math.abs(p[1]+.38)-.07))], .8)
  // A chubby teapot: a big flat-bottomed pot, short curved spout and a tight handle, so it fills its tile.
  const pot = p => Math.max(oval([0,-.06,0],[.74,.64,.6])(p),-.62-p[1])
  const lid = union(oval([0,.54,0],[.44,.12,.38]),sphere([0,.7,0],.11))
  const handle = torus([.76,-.02,0],.28,.1)
  const spout = smin(.06,roundCone([-.5,-.22,0],[-.84,-.04,0],.22,.13),roundCone([-.84,-.04,0],[-.98,.2,0],.13,.11))
  const trim = p => Math.min(handle(p),spout(p))
  set('teapot',smin(.05,pot,lid,handle,spout), [[-.23,-.02],[.23,-.02]], [
    paint('#4C9694',p => lid(p)-pot(p),true),
    paint('#D6E7C8',p => Math.max(pot(p)-trim(p),Math.abs(p[1]+.34)-.08),true)], .8)
  return models
}
