// Additional Polymux characters, in the same y-up signed-distance space as avatar-designs.mjs.
import { rounded } from './avatar-curves.mjs'
export const colors = {
  lemon: '#E9C858', orangeFruit: '#ED9C42', watermelon: '#E15368', avocado: '#91AD68',
  cherry: '#BD566B', cookie: '#C99D6B', cheese: '#E7BF66', egg: '#EFE5D2', candy: '#BD91CA', donut: '#D6A574',
  tulip: '#D887AD', sunflower: '#F0C052', sprout: '#80AB75', fern: '#71A08B', balloon: '#D98073',
  shell: '#D6AAA0', palm: '#AC8056', snowman: '#8197A4', lightning: '#AB7A24', planet: '#A17D61',
  mug: '#89B7C2', lantern: '#D9B76A', gift: '#BB8CB5', pencil: '#E0B65D', hammer: '#8194A3',
  umbrella: '#92ACC9', wateringCan: '#5C998A', gamepad: '#918CB7', guitar: '#C89D73', telescope: '#90ACBD',
  panda: '#EEEDE6', crab: '#D56B56', elephant: '#8494AF', octopus: '#A36BAE', giraffe: '#B38A43', walrus: '#AC8774',
  turtle: '#6AA078', bat: '#9B87B3', koala: '#9A96A0', frog: '#79B84E', fox: '#E0773A',
  dog: '#C39460', rabbit: '#AC859C', penguin: '#303944', axolotl: '#F0B6C4', mouse: '#AAA3A0',
}

export function expand(models, tools) {
  const { ellipsoid: oval, sphere, capsule, roundCone, roundBox: box, extrude, polygonDistance, torus, smin } = tools
  const union = (...parts) => p => Math.min(...parts.map(f => f(p)))
  const slab = (points, depth = .25) => {
    const outline = rounded(points, .24)
    return extrude((x,y) => polygonDistance([outline],x,y),depth,.1)
  }
  const lathe = points => p => polygonDistance([points],Math.hypot(p[0],p[2]),p[1])-.04
  const band = (fill, test) => ({fill, region: p => test(...p) ? -1 : 1, wrap: true})
  const spot = (fill,x,y,rx,ry) => ({fill, region: p => Math.hypot((p[0]-x)/rx,(p[1]-y)/ry)-1})
  const set = (id,sdf,paints=[],eyes=[[-.21,.04],[.21,.04]],eye=.72) => { models[id]={sdf,paints,eyes,eye} }
  const stem = () => capsule([0,.65,0],[.08,1,0],.075)
  const leaf = (x,y,angle=30) => oval([x,y,0],[.3,.12,.08],angle)
  set('lemon',union(oval([0,0,0],[.86,.65,.57]),sphere([-.83,0,0],.13),sphere([.83,0,0],.13)),[
    spot('#F5DE86',-.46,.3,.15,.08)])
  const orangeBody = oval([0,-.1,0],[.8,.73,.72])
  const orangeTop = union(capsule([0,.61,0],[.035,.83,0],.055),leaf(.26,.75,25))
  const pores = Array.from({length:84},(_,i)=>{
    const y=1-2*(i+.5)/84, r=Math.sqrt(1-y*y), a=i*2.39996323
    return [.8*r*Math.cos(a),-.1+.73*y,.72*r*Math.sin(a)]
  }).filter(([x,y,z])=>z<.25||Math.abs(x)>.38||y<-.22||y>.38)
  set('orangeFruit',union(orangeBody,orangeTop),[
    band('#D98730',(x,y,z)=>orangeBody([x,y,z])<orangeTop([x,y,z])&&
      pores.some(([a,b,c])=>Math.hypot(x-a,y-b,z-c)<.038)),
    band('#719369',(...p)=>orangeTop(p)<orangeBody(p))])
  const melonRadius = (x,y) => Math.hypot(x,y-.35)
  set('watermelon',extrude((x,y)=>Math.max(melonRadius(x,y)-1.02,y-.35),.13,.06),[
    band('#398257',(x,y)=>melonRadius(x,y)>.9),
    band('#DCE8A5',(x,y)=>melonRadius(x,y)>.77&&melonRadius(x,y)<=.9),
    ...[[-.55,.08],[.55,.08],[-.34,-.23],[.34,-.23],[0,-.3]].map(([x,y])=>spot('#49323A',x,y,.045,.075))],
    [[-.18,.13],[.18,.13]],.65)
  const avocado = (x,y) => .6*(Math.hypot(x/(.63-.18*Math.max(-1,Math.min(1,y/.94))),y/.94)-1)
  set('avocado',extrude(avocado,.16,.08),[
    band('#4E795B',(x,y,z)=>z<.08||avocado(x,y)>-.09),
    spot('#A77954',0,-.34,.33,.34),spot('#BD9366',-.09,-.25,.18,.2)], [[-.18,.3],[.18,.3]])
  set('cherry',union(sphere([-.43,-.32,0],.52),sphere([.43,-.32,0],.52),
    capsule([-.43,.1,0],[0,.92,0],.055),capsule([.43,.1,0],[0,.92,0],.055),leaf(.25,.86)),[
    band('#749366',(x,y)=>y>.21)], [[-.52,-.22],[-.25,-.22]],.58)
  set('cookie',oval([0,0,0],[.85,.85,.26]),[
    ...[[-.48,.48],[.4,.5],[.59,-.17],[-.54,-.26],[0,-.52]].map(([x,y])=>spot('#755541',x,y,.1,.09))])
  set('cheese',slab([[-.82,-.62],[.78,-.62],[.78,.2],[-.62,.76],[-.82,.54]],.32),[
    band('#D5A94F',(x,y,z)=>z<.18),spot('#C1953D',-.5,.37,.14,.14),
    spot('#C1953D',.48,-.3,.17,.15),spot('#C1953D',-.39,-.35,.1,.1)])
  const egg = (x,y) => .65*(Math.hypot(x/.83,y/.88)-1-.07*Math.sin(Math.atan2(y,x)*3)-.04*Math.cos(Math.atan2(y,x)*5))
  set('egg',union(extrude(egg,.13,.08),oval([0,-.12,.17],[.47,.46,.17])),[
    spot('#EDBE54',0,-.12,.46,.45),spot('#F8D879',-.19,.04,.1,.07)], [[-.16,-.08],[.16,-.08]],.62)
  set('candy',smin(.1,oval([0,0,0],[.57,.55,.42]),
    slab([[-.45,0],[-.95,.32],[-.87,0],[-.95,-.32]],.12),
    slab([[.45,0],[.95,.32],[.87,0],[.95,-.32]],.12)),[
    band('#E4C1D9',(x,y)=>Math.abs(x)>.59||Math.abs(x+y*.5)<.12)])
  set('donut',torus([0,0,0],.57,.3),[
    band('#D995AF',(x,y,z)=>z>.12),...[[.4,.4],[-.5,.25],[.15,-.6],[-.4,-.4]].map(([x,y])=>spot('#F2D589',x,y,.08,.035))],
    [[-.18,.59],[.18,.59]],.56)
  set('tulip',smin(.08,oval([0,.24,0],[.62,.55,.38]),
    oval([-.4,.59,0],[.23,.38,.25],-18),oval([.4,.59,0],[.23,.38,.25],18),
    oval([0,.64,0],[.26,.35,.3]),capsule([0,-.85,0],[0,-.1,0],.07),leaf(.23,-.53)),[
    band('#6E996C',(x,y)=>y<-.25),spot('#E9AAC2',-.32,.51,.18,.34)], [[-.18,.25],[.18,.25]])
  const sunflowerPetals = union(...Array.from({length:12},(_,i)=>{
    const a=i*Math.PI/6
    return oval([.64*Math.cos(a),.24+.64*Math.sin(a),-.02],[.29,.13,.1],i*30)
  }))
  const sunflowerFace = oval([0,.24,.11],[.5,.5,.23])
  const sunflowerStem = union(capsule([0,-.1,-.07],[0,-1.12,-.07],.065),
    oval([-.25,-.82,-.07],[.33,.13,.08],-30),oval([.23,-.94,-.07],[.31,.12,.08],30),
    oval([0,.24,-.12],[.46,.46,.15]))
  const sunflowerParts = [sunflowerPetals,sunflowerFace,sunflowerStem]
  const sunflowerNearest = part=>(...p)=>part(p)<=Math.min(...sunflowerParts.map(f=>f(p)))
  set('sunflower',union(...sunflowerParts),[
    band('#DFA337',(x,y,z)=>z>-.015&&sunflowerNearest(sunflowerPetals)(x,y,z)&&Math.hypot(x,y-.24)<.64),
    band('#B98B5B',sunflowerNearest(sunflowerFace)),
    band('#6E9961',sunflowerNearest(sunflowerStem))],[[-.18,.25],[.18,.25]],.58)
  set('sprout',union(sphere([0,-.42,0],.49),capsule([0,-.1,0],[0,.64,0],.08),
    oval([-.36,.48,0],[.45,.2,.15],-32),oval([.36,.69,0],[.45,.2,.15],32)),[
    band('#B39A76',(x,y)=>y<-.08)], [[-.12,-.4],[.12,-.4]],.5)
  set('fern',union(oval([0,-.46,0],[.43,.47,.3]),capsule([0,-.2,0],[0,.96,0],.06),
    ...[0,.32,.64].flatMap(y=>[-1,1].map(s=>oval([s*(.37-y*.2),y+.1,0],[.42-y*.2,.1,.1],s*30)))),[],
    [[-.14,-.4],[.14,-.4]],.6)
  const envelope = lathe(rounded([[0,1.12],[.32,1.06],[.62,.85],[.78,.52],
    [.77,.24],[.65,-.1],[.42,-.43],[.19,-.63],[.16,-.72],[0,-.72]],.14))
  const ropes = union(...[-1,1].flatMap(x=>[-1,1].map(z=>
    capsule([x*.13,-.65,z*.13],[x*.21,-1.03,z*.17],.04))))
  const basket = p=>Math.max(box([0,-1.15,0],[.27,.18,.23],.055)(p),
    -box([0,-.99,0],[.2,.22,.16],.025)(p))
  const gore = (x,z)=>(Math.floor((Math.atan2(x,z)+Math.PI/6)/(Math.PI/3))+6)%3
  set('balloon',union(envelope,ropes,basket),[
    band('#F3D9A4',(x,y,z)=>y>-.76&&gore(x,z)===0),
    band('#78AAA6',(x,y,z)=>y>-.76&&gore(x,z)===1),
    band('#785844',(x,y)=>y<-.76),
    band('#BB8C5C',(x,y,z)=>y<-.96&&basket([x,y,z])<ropes([x,y,z]))],
    [[-.23,.38],[.23,.38]],.68)
  const fan = [[0,-.69],...Array.from({length:65},(_,i)=>{
    const a=(-65+i*130/64)*Math.PI/180, r=1.26+.035*Math.cos(i/64*Math.PI*20)
    return [Math.sin(a)*r,-.69+Math.cos(a)*r]
  })]
  const clam = ([x,y,z])=>{
    const r=Math.hypot(x,y+.69), a=Math.atan2(x,y+.69)
    const depth=.08+.25*Math.sin(Math.min(1,r/1.34)*Math.PI)+.035*Math.cos(a*28)
    return Math.max(polygonDistance([fan],x,y),Math.abs(z)-depth)
  }
  const hinge = oval([0,-.67,0],[.32,.13,.13])
  set('shell',union(clam,hinge),[
    ...Array.from({length:10},(_,i)=>band(i%2?'#DCAAA0':'#EAC5B4',
      (x,y)=>y>-.62&&Math.atan2(x,y+.69)>-.95+i*.21)),
    band('#EAC8B7',(x,y,z)=>hinge([x,y,z])<clam([x,y,z]))],[[-.19,-.13],[.19,-.13]],.62)
  const fronds = [-1,1].flatMap(s => [
    [[.04,.56],[s*.27,1.03],[s*.72,1.08],[s*.43,.81],[s*.2,.51]],
    [[.04,.55],[s*.47,.86],[s*.88,.73],[s*1.13,.25],[s*.71,.52],[s*.34,.55]],
    [[.04,.55],[s*.4,.46],[s*.66,.08],[s*.64,-.18],[s*.41,.05],[s*.18,.39]]
  ].map(points => extrude((x,y)=>polygonDistance([rounded(points,.06)],x,y),.085,.035)))
  const palmTrunk = roundCone([-.15,-.95,0],[.06,.43,0],.21,.15)
  const palmLeaves = union(...fronds,oval([.04,.52,.1],[.27,.22,.23]))
  set('palm',union(palmTrunk,palmLeaves),[
    band('#805C3F',(x,y,z)=>y<.25&&x<-.07&&z<.1),
    band('#6D4C35',(x,y,z)=>(y<-.48||y>.05)&&y<.18&&z>.06&&Math.abs(Math.sin((y+x*.25)*10))<.16),
    band('#4D9969',(x,y,z)=>y>.34||palmLeaves([x,y,z])<palmTrunk([x,y,z])),
    band('#337650',(x,y,z)=>(y>.34||palmLeaves([x,y,z])<palmTrunk([x,y,z]))&&y<.48)],
    [[-.14,-.26],[-.03,-.26]],.26)
  set('snowman',union(sphere([0,-.42,0],.64),sphere([0,.43,0],.45),
    box([0,.9,0],[.56,.07,.39],.035),box([0,1.09,0],[.32,.22,.3],.035),
    roundCone([.02,.31,.35],[.28,.29,.64],.1,.015)),[
    spot('#E3EBE7',0,-.42,.49,.49),spot('#E3EBE7',0,.43,.33,.33),
    band('#4D566B',(x,y)=>y>.85),band('#B85E62',(x,y)=>y>.92&&y<1.02||y>-.02&&y<.13),
    band('#B85E62',(x,y,z)=>x>.2&&x<.35&&y<.08&&y>-.46&&z>.32),
    band('#DE8C42',(x,y,z)=>z>.4&&y>.19&&y<.4),
    spot('#4D566B',0,-.33,.065,.065),spot('#4D566B',0,-.63,.065,.065)], [[-.11,.4],[.11,.4]],.44)
  const bolt = rounded([[.12,1.05],[.78,1.05],[.25,.24],[.83,.24],[-.65,-1.05],[-.24,-.17],[-.78,-.17]],.035)
  set('lightning',extrude((x,y)=>polygonDistance([bolt],x,y),.12,.045),[
    band('#EAC14F',(x,y,z)=>z>.13)], [[-.19,.08],[.12,.08]],.55)
  set('planet',union(sphere([0,0,0],.65),p=>torus([0,0,0],.94,.105)([p[0],p[2],p[1]])),[
    band('#8D695D',(x,y)=>y<-.26||y>.4),band('#AC8745',(x,y,z)=>Math.hypot(x,z)>.77)], [[-.15,.22],[.15,.22]],.52)
  const cup = lathe([[0,-.68],[.45,-.68],[.58,-.55],[.6,.62],[.47,.62],[.44,-.43],[0,-.43]])
  set('mug',union(cup,torus([.61,-.03,0],.34,.12)),[
    band('#D7E6DB',(x,y)=>y>.5),band('#4F7F88',(x,y,z)=>y>-.45&&Math.hypot(x,z)<.45)], [[-.2,-.02],[.2,-.02]])
  const roof = slab([[-.65,.59],[.65,.59],[.36,.87],[-.36,.87]],.65)
  set('lantern',union(box([0,-.03,0],[.55,.59,.55],.24),box([0,-.7,0],[.65,.1,.65],.09),
    p=>Math.max(roof(p),roof([p[2],p[1],p[0]])),torus([0,.92,0],.18,.065)),[
    band('#758A7A',(x,y,z)=>y<-.55||y>.55||Math.abs(x)>.43&&Math.abs(z)>.43)], [[-.18,0],[.18,0]])
  set('gift',union(box([0,-.12,0],[.7,.67,.7],.22),box([0,.54,0],[.77,.12,.77],.11),
    sphere([0,.72,0],.12),oval([-.27,.83,0],[.3,.17,.15],-25),oval([.27,.83,0],[.3,.17,.15],25)),[
    band('#E9C98C',(x,y,z)=>Math.abs(x)<.09||Math.abs(z)<.09||y>.67)], [[-.3,-.08],[.3,-.08]])
  set('pencil',lathe([[0,-1.04],[.24,-.59],[.24,.79],[.18,.88],[0,.88]]),[
    band('#D6969B',(x,y)=>y>.59),band('#B1BDC0',(x,y)=>y>.42&&y<.59),
    band('#E4CA9D',(x,y)=>y<-.6),band('#54525B',(x,y)=>y<-.91)], [[-.07,.1],[.07,.1]],.4)
  set('hammer',union(capsule([.05,-.95,0],[.05,.49,0],.16),box([.31,.48,0],[.43,.27,.3],.06),
    slab([[-.04,.74],[-.61,.7],[-.86,.28],[-.42,.42],[-.04,.25]],.25)),[
    band('#C09564',(x,y)=>y<.22),band('#725540',(x,y)=>y<-.64),
    band('#C6D1D5',(x,y)=>x>.61&&y>.2)], [[.13,.48],[.44,.48]],.6)
  // A round canopy of eight scalloped panels, alternating colours.
  const panel = (x,z) => Math.cos(4*Math.atan2(z,x))
  const hem = p=>-.08+.1*Math.abs(panel(p[0],p[2]))-p[1]
  const canopy = p=>Math.max(oval([0,.13,0],[.97,.72,.97])(p),hem(p))-.035
  set('umbrella',union(canopy,capsule([0,-.66,0],[0,.25,0],.065),
    p=>Math.max(torus([.17,-.66,0],.17,.065)(p),p[1]+.66),capsule([.34,-.66,0],[.34,-.55,0],.065)),[
    band('#D0C1D7',(x,y,z)=>canopy([x,y,z])<.01&&panel(x,z)>0),
    band('#7D93AE',(...p)=>canopy(p)<.01&&hem(p)>oval([0,.13,0],[.97,.72,.97])(p)-.02),
    band('#77898D',(x,y)=>y<-.13)], [[-.22,.42],[.22,.42]])
  set('wateringCan',union(box([0,-.15,0],[.53,.55,.5],.18),torus([.68,.05,0],.36,.1),
    roundCone([-.43,-.43,0],[-.96,.35,0],.2,.115),oval([-.98,.4,0],[.23,.12,.24],-35),
    oval([0,.42,0],[.38,.07,.38])),[
    band('#416E67',(x,y)=>y>.32||y<-.57),spot('#A9D0B2',-.22,.06,.1,.06)], [[-.19,-.12],[.19,-.12]],.72)
  set('gamepad',slab([[-.75,.48],[.75,.48],[.99,-.52],[.7,-.75],[.33,-.3],[-.33,-.3],[-.7,-.75],[-.99,-.52]],.3),[
    band('#64607F',(x,y,z)=>Math.abs(z)<.22),
    band('#45465D',(x,y,z)=>z>.2&&Math.min(Math.max(Math.abs(x+.58)-.19,Math.abs(y)-.06),
      Math.max(Math.abs(x+.58)-.06,Math.abs(y)-.19))<0),
    spot('#527F71',.59,.17,.08,.08),spot('#B5656C',.77,0,.08,.08),
    spot('#D4B56C',.59,-.17,.08,.08),spot('#5A7DAB',.41,0,.08,.08)], [[-.16,.13],[.16,.13]],.62)
  set('guitar',union(smin(.13,oval([0,-.54,0],[.6,.51,.29]),oval([0,-.03,0],[.42,.36,.25])),
    box([0,.55,0],[.12,.55,.12],.07),box([0,1.01,0],[.19,.21,.13],.1),
    ...[-1,1].flatMap(s=>[.9,1.08].map(y=>capsule([s*.16,y,0],[s*.28,y,0],.055)))),[
    spot('#684C39',0,-.42,.21,.22),
    band('#E9D2A4',(x,y)=>y>-.7&&y<.89&&[-.045,0,.045].some(line=>Math.abs(x-line)<.011)),
    band('#79563E',(x,y)=>Math.abs(x)<.22&&y>-.79&&y<-.7)], [[-.12,-.09],[.12,-.09]],.47)
  const telescope = lathe([[0,-.89],[.31,-.89],[.37,-.78],[.37,-.57],[.27,-.57],[.22,.58],[.16,.58],[.16,.83],[0,.83]])
  set('telescope',union(p=>telescope([p[1]-.18,p[0],p[2]]),
    capsule([0,-.1,0],[-.46,-.9,0],.065),capsule([0,-.1,0],[.46,-.9,0],.065),
    capsule([0,-.1,0],[0,-.84,.3],.06)),[
    band('#CCD7D8',(x,y)=>x<-.58),band('#426375',(x,y,z)=>x<-.86),
    band('#718796',(x,y)=>y<-.2||x>.58)], [[-.2,.2],[.17,.2]],.65)

  // A chubby cream panda: round black ears, big droopy eye patches, a little muzzle, blush and a tiny smile.
  const pandaHead = smin(.25,oval([0,-.02,0],[.78,.68,.6]),oval([0,-.3,.1],[.72,.4,.52]))
  const pandaEars = union(sphere([-.56,.53,-.05],.27),sphere([.56,.53,-.05],.27))
  const pandaMuzzle = oval([0,-.3,.46],[.27,.18,.16])
  const pandaNose = oval([0,-.21,.6],[.1,.065,.06])
  const pandaParts = [pandaHead,pandaEars,pandaMuzzle,pandaNose]
  const pandaNearest = part => (...p) => part(p) <= Math.min(...pandaParts.map(f=>f(p)))
  const pandaFace = (...p) => !pandaNearest(pandaEars)(...p) && !pandaNearest(pandaNose)(...p)
  // Each patch leans its top inward and widens towards the cheek.
  const pandaPatch = (x,y) => {
    const s=Math.sign(x), dx=x-s*.3, dy=y+.02, v=-dx*s*.5+dy*.866, u=dx*.866+dy*s*.5
    return Math.hypot(u/(.2-.045*v/.27),v/.27) < 1
  }
  set('panda',union(...pandaParts),[
    band('#CFD1CB',(x,y,z)=>z<-.12&&pandaFace(x,y,z)),
    band('#2F363B',pandaNearest(pandaEars)),
    band('#2F363B',(x,y,z)=>z>.15&&pandaFace(x,y,z)&&pandaPatch(x,y)),
    band('#F2B9BF',(x,y,z)=>z>.3&&pandaFace(x,y,z)&&Math.hypot((Math.abs(x)-.5)/.1,(y+.3)/.065)<1),
    band('#2F363B',pandaNearest(pandaNose)),
    band('#2F363B',(x,y,z)=>z>.5&&(Math.abs(x)<.016&&y<-.26&&y>-.34||
      Math.abs(x)<.12&&Math.abs(y+.36-3.2*(Math.abs(x)-.06)**2)<.016))], [[-.28,.01],[.28,.01]],.54)
  models.panda.eyeColor = '#F3EDDF'
  // Apple's fox: a white muzzle framed in orange, a black nose, and dark-tipped ears with pale insides.
  const foxHead = smin(.08,oval([0,0,0],[.64,.56,.5]),oval([0,-.18,.4],[.2,.15,.3]))
  const foxEar = [[.12,.36],[.6,.3],[.46,.98]]
  const foxEars = union(...[-1,1].map(s=>{
    const ear = rounded(foxEar.map(([x,y])=>[s*x,y]),.06)
    return p=>extrude((x,y)=>polygonDistance([ear],x,y),.07,.07)([p[0],p[1],p[2]+.04])
  }))
  const foxNose = sphere([0,-.16,.72],.075)
  const foxParts = [foxHead,foxEars,foxNose]
  const foxNearest = part => (...p) => part(p) <= Math.min(...foxParts.map(f=>f(p)))
  const foxInner = rounded(foxEar.map(([x,y])=>[.39+(x-.39)*.5,.55+(y-.55)*.5]),.03)
  set('fox',smin(.05,foxHead,foxEars,foxNose),[
    band('#FFF4E4',(x,y,z)=>foxNearest(foxHead)(x,y,z)&&z>.2&&y<-.2+.55*Math.abs(x)-.15*Math.max(0,z-.3)),
    band('#F8D9BC',(x,y,z)=>foxNearest(foxEars)(x,y,z)&&z>0&&polygonDistance([foxInner],Math.abs(x),y)<0),
    band('#4A2C22',(x,y,z)=>foxNearest(foxEars)(x,y,z)&&y>.8),
    band('#2B201D',foxNearest(foxNose))], [[-.22,.12],[.22,.12]],.55)
  // Apple's koala: big round ears high on the head and a large dark nose.
  const koalaHead = oval([0,-.06,0],[.76,.66,.56])
  const koalaEars = smin(.06,...[-1,1].flatMap(s=>[oval([s*.64,.42,-.08],[.32,.3,.2]),
    ...[-50,-15,20,55,90,125].map(a=>{
      const r=a*Math.PI/180
      return sphere([s*(.64+Math.cos(r)*.29),.42+Math.sin(r)*.27,-.08],.09)
    })]))
  const koalaNose = oval([0,-.2,.5],[.15,.21,.13])
  set('koala',union(koalaHead,koalaEars,koalaNose),[
    ...[-1,1].map(s=>band('#E3DCE0',(x,y,z)=>z>.04&&koalaEars([x,y,z])<koalaHead([x,y,z])&&
      Math.hypot((x-s*.68)/.2,(y-.44)/.18)<1)),
    band('#2E2A2F',(x,y,z)=>koalaNose([x,y,z])<koalaHead([x,y,z]))], [[-.27,-.03],[.27,-.03]],.5)
  // A frog: a wide green head with googly eyes on raised domes, a wide smile and pink cheeks.
  const frogHead = oval([0,-.12,0],[.84,.6,.56])
  const frogDomes = union(...[-1,1].map(s=>sphere([s*.4,.36,.14],.31)))
  const frogNearest = part => (...p) => part(p) <= Math.min(frogHead(p),frogDomes(p))
  set('frog',union(frogHead,frogDomes),[
    band('#B9DC86',(x,y,z)=>frogNearest(frogHead)(x,y,z)&&y<-.55),
    band('#F2A0A0',(x,y,z)=>z>.2&&frogNearest(frogHead)(x,y,z)&&Math.hypot((Math.abs(x)-.52)/.11,(y+.2)/.075)<1),
    band('#2F5A2B',(x,y,z)=>z>.3&&frogNearest(frogHead)(x,y,z)&&Math.abs(x)<.42&&Math.abs(y+.38-.35*x*x)<.03),
    band('#2F5A2B',(x,y,z)=>z>.4&&Math.hypot(Math.abs(x)-.07,y+.13)<.025),
    band('#F7F4E8',(x,y,z)=>z>.14&&frogNearest(frogDomes)(x,y,z)&&Math.hypot(Math.abs(x)-.4,y-.36)<.22)],
    [[-.4,.36],[.4,.36]],.5)
  const claws = [-1,1].map(s => p => Math.max(oval([s*.86,.5,0],[.29,.35,.23],s*25)(p),
    -polygonDistance([[[s*.86,.49],[s*.66,.88],[s*1.01,.88]]],p[0],p[1])))
  set('crab',union(oval([0,-.15,0],[.65,.45,.37]),...claws,
    ...[-1,1].flatMap(s => [capsule([s*.48,-.03,0],[s*.82,.31,0],.09),
      ...[-.16,-.4,-.61].map(y=>capsule([s*.45,y+.13,0],[s*.91,y,0],.07))])),[
    band('#EEA17B',(x,y)=>y>.45),spot('#E88D71',0,-.42,.4,.12)], [[-.2,-.12],[.2,-.12]],.62)
  const trunk = smin(.06,roundCone([0,-.22,.43],[0,-.88,.48],.21,.14),
    capsule([0,-.88,.48],[.24,-.91,.48],.14),capsule([.24,-.91,.48],[.33,-.75,.48],.13))
  set('elephant',union(oval([0,.08,0],[.57,.67,.55]),oval([-.66,.06,-.13],[.4,.61,.22]),
    oval([.66,.06,-.13],[.4,.61,.22]),trunk),[
    band('#B9B1C8',(x,y,z)=>Math.abs(x)>.62&&z>.015),
    band('#6B7F9E',(x,y,z)=>y<-.48&&z>.45&&Math.abs(Math.sin(y*20))<.25)], [[-.24,.17],[.24,.17]],.68)
  const tentacles = [
    [[-.45,-.12,-.12],[-.88,-.24,-.25],[-1.23,-.17,-.14],[-1.12,.16,-.03]],
    [[-.38,-.23,.08],[-.65,-.67,.04],[-1.2,-.65,.2],[-1.13,-.44,.28]],
    [[-.22,-.25,.32],[-.38,-.82,.48],[-.68,-.99,.62],[-.75,-.74,.65]],
    [[-.06,-.28,.32],[-.06,-.92,.62],[.19,-1.12,.66],[.28,-1,.57]],
    [[.4,-.16,-.18],[.78,-.08,-.38],[1.16,.01,-.35],[1.1,.19,-.22]],
    [[.42,-.2,.04],[.87,-.57,.05],[1.19,-.48,.08],[1.21,-.23,.19]],
    [[.24,-.26,.29],[.54,-.72,.45],[.91,-.84,.47],[.86,-.54,.59]],
    [[-.02,-.28,-.31],[.18,-.85,-.49],[.56,-1,-.42],[.66,-.78,-.28]]
  ].map(points=>Array.from({length:10},(_,i)=>{
    const t=i/9, u=1-t
    return [0,1,2].map(k=>(u**3*points[0][k]+3*u*u*t*points[1][k]+3*u*t*t*points[2][k]+t**3*points[3][k])*(k===2 ? .35 : 1))
  }))
  const radius = t=>.135*(1-t)**.75+.035
  const arms = tentacles.flatMap(points=>points.slice(1).map((p,i)=>
    roundCone(points[i],p,radius(i/9),radius((i+1)/9))))
  set('octopus',smin(.025,oval([0,.36,0],[.55,.61,.53]),...arms),[
    spot('#BE91C6',-.25,.69,.12,.07)],
    [[-.18,.37],[.18,.37]],.55)
  const giraffeHead = oval([0,.02,0],[.5,.73,.5])
  const giraffeMuzzle = oval([0,-.48,.25],[.42,.3,.4])
  const giraffeEars = union(...[-1,1].map(s=>oval([s*.55,.43,0],[.3,.17,.22],s*24)))
  const giraffeHorns = union(...[-1,1].flatMap(s=>[
    capsule([s*.24,.57,0],[s*.28,.99,0],.075),sphere([s*.28,1.02,0],.12)]))
  const giraffeSpots = [[-.38,.25,.32],[.38,.25,.32],[0,.56,.35],[-.43,-.15,.24],[.43,-.15,.24],
    [-.44,.12,-.2],[.44,.12,-.2],[-.21,.44,-.37],[.21,.44,-.37],[0,-.1,-.48],[-.22,-.43,-.32],[.22,-.43,-.32]]
  set('giraffe',smin(.055,giraffeHead,giraffeMuzzle,giraffeEars,giraffeHorns),[
    band('#7A5439',(x,y)=>y>.93),
    band('#95613A',(x,y,z)=>giraffeHead([x,y,z])<giraffeMuzzle([x,y,z])&&
      giraffeSpots.some(([a,b,c])=>Math.hypot(x-a,(y-b)*.9,z-c)<.14)),
    band('#E9CE8D',(x,y,z)=>giraffeMuzzle([x,y,z])<giraffeHead([x,y,z])),
    band('#DAB879',(x,y,z)=>z>.1&&giraffeEars([x,y,z])<giraffeHead([x,y,z])&&
      Math.hypot((Math.abs(x)-.59)/.17,(y-.45)/.075)<1),
    band('#79543D',(x,y,z)=>z>.5&&Math.hypot((Math.abs(x)-.14)/.045,(y+.43)/.035)<1)],
    [[-.17,.13],[.17,.13]],.58)
  const tusks = [-1,1].map(s=>roundCone([s*.24,-.33,.43],[s*.29,-1,.43],.11,.035))
  const muzzle = union(oval([-.19,-.25,.35],[.29,.23,.25]),oval([.19,-.25,.35],[.29,.23,.25]))
  set('walrus',union(oval([0,.03,0],[.78,.64,.54]),muzzle,...tusks),[
    band('#F4E6C5',(x,y,z)=>Math.min(...tusks.map(t=>t([x,y,z])))<muzzle([x,y,z])&&y<-.4),
    band('#D7B899',(x,y,z)=>z>.43&&y<-.1&&y>-.43),spot('#5C4640',0,-.14,.14,.08),
    ...[-1,1].flatMap(s=>[-.22,-.35].map(y=>spot('#74574B',s*.36,y,.16,.025)))], [[-.24,.17],[.24,.17]],.72)
  const shell = oval([0,-.28,-.2],[.66,.68,.46])
  const turtleBody = oval([0,-.32,.17],[.46,.56,.34])
  const turtleHead = oval([0,.55,.22],[.38,.35,.35])
  const turtleLimbs = union(...[-1,1].flatMap(s=>[
    oval([s*.52,-.19,.15],[.18,.3,.18],s*28),oval([s*.3,-.85,.2],[.2,.13,.24])]))
  const scute = [[-.28,-.12],[-.28,-.43],[0,-.6],[.28,-.43],[.28,-.12],[0,.04]]
  const plates = scute.map((point,i)=>{
    const next=scute[(i+1)%scute.length]
    const points=[point,next,[next[0]*2,(next[1]+.28)*2-.28],[point[0]*2,(point[1]+.28)*2-.28]]
    const center=[0,1].map(axis=>points.reduce((sum,p)=>sum+p[axis],0)/4)
    return points.map(p=>p.map((v,axis)=>center[axis]+(v-center[axis])*.88))
  })
  const belly = (x,y,z)=>z>.34&&Math.hypot(x/.36,(y+.32)/.45)<1&&turtleBody([x,y,z])<turtleHead([x,y,z])
  set('turtle',smin(.04,shell,turtleBody,turtleHead,turtleLimbs),[
    band('#3E7055',(x,y,z)=>shell([x,y,z])<Math.min(turtleBody([x,y,z]),turtleHead([x,y,z]),turtleLimbs([x,y,z]))),
    band('#97BD7C',(x,y,z)=>z<-.25&&polygonDistance([scute],x,y)<0),
    ...plates.map(plate=>band('#729E71',(x,y,z)=>z<-.25&&polygonDistance([plate],x,y)<0)),
    band('#D3CE91',belly),
    ...[-.1,-.34,-.57].flatMap(line=>[
      band('#ACA96F',(x,y,z)=>belly(x,y,z)&&y<line+.018),
      band('#D3CE91',(x,y,z)=>belly(x,y,z)&&y<line-.018)]),
    band('#ACA96F',(x,y,z)=>belly(x,y,z)&&Math.abs(x)<.02),
    band('#2E5A40',(x,y,z)=>z>.48&&turtleHead([x,y,z])<turtleBody([x,y,z])&&Math.abs(x)<.1&&Math.abs(y-.4-2*x*x)<.014)],
    [[-.13,.59],[.13,.59]],.44)
  const wings = [-1,1].map(s=>{
    const outline = rounded([[.2,.27],[.53,.71],[.86,.68],[1.2,.43],[1.39,-.05],
      [1.24,.05],[1.09,.1],[1,.07],[.92,-.06],[.88,-.28],
      [.77,-.15],[.64,-.13],[.52,-.2],[.42,-.46],[.25,-.34],[.16,-.38]]
      .map(([x,y])=>[s*x,y]),.045)
    const wing = extrude((x,y)=>polygonDistance([outline],x,y),.065,.025)
    return p=>wing([p[0],p[1],p[2]+.1])
  })
  const ribs = union(...[[[.27,.25],[.53,.57]],[[.53,.57],[1.31,.02]],
    [[.53,.57],[.88,-.23]],[[.53,.57],[.43,-.4]]].map(([a,b])=>capsule([...a,0],[...b,0],.025)))
  set('bat',union(oval([0,-.12,.06],[.28,.47,.31]),oval([0,.29,.06],[.34,.32,.32]),...wings,
    ...[-1,1].map(s=>{
      const ear = rounded([[s*.11,.43],[s*.12,.72],[s*.35,.98],[s*.31,.41]],.025)
      return extrude((x,y)=>polygonDistance([ear],x,y),.15,.025)
    })),[
    band('#7D6995',(x,y)=>Math.abs(x)>.35),
    band('#A18BB8',(x,y,z)=>Math.abs(x)>.36&&z>-.055&&ribs([Math.abs(x),y,0])<0),
    spot('#D3AEC7',-.24,.69,.055,.16),spot('#D3AEC7',.24,.69,.055,.16),
    ...[-1,1].map(s=>band('#EFE3D3',(x,y,z)=>z>.26&&y<.04&&y>-.09&&Math.abs(x-s*.085)<(y+.09)*.28))],
    [[-.13,.29],[.13,.29]],.52)
  // A tan dog with upright ears, a pale muzzle and a patch over one eye.
  const dogHead = oval([0,.02,0],[.66,.62,.56])
  const dogEars = union(...[-1,1].map(s=>{
    const outline = rounded([[s*.25,.3],[s*.7,.26],[s*.61,.84]],.09)
    return extrude((x,y)=>polygonDistance([outline],x,y),.12,.045)
  }))
  const dogMuzzle = oval([0,-.3,.4],[.33,.24,.26])
  const dogNose = oval([0,-.17,.66],[.13,.09,.08])
  const dogParts = [dogHead,dogEars,dogMuzzle,dogNose]
  const dogNearest = part => (...p) => part(p) <= Math.min(...dogParts.map(f=>f(p)))
  set('dog',smin(.05,...dogParts),[
    band('#7A5134',dogNearest(dogEars)),
    band('#9B6A43',(x,y,z)=>z>.2&&dogNearest(dogHead)(x,y,z)&&Math.hypot((x-.27)/.2,(y-.15)/.22)<1),
    band('#F3E1C3',dogNearest(dogMuzzle)),
    band('#2B201D',dogNearest(dogNose)),
    band('#2B201D',(x,y,z)=>z>.5&&(Math.abs(x)<.016&&y<-.24&&y>-.36||
      Math.abs(x)<.17&&Math.abs(y+.38-2.6*(Math.abs(x)-.08)**2)<.017))], [[-.25,.15],[.25,.15]],.6)
  // A rabbit: tall ears with pink insides, puffy white cheeks, a pink nose and two front teeth.
  const rabbitHead = oval([0,-.26,0],[.62,.55,.52])
  const rabbitEars = union(...[-1,1].map(s=>oval([s*.25,.62,-.06],[.15,.45,.12],s*-8)))
  const rabbitCheeks = union(...[-1,1].map(s=>sphere([s*.11,-.45,.4],.14)))
  const rabbitParts = [rabbitHead,rabbitEars,rabbitCheeks]
  const rabbitNearest = part => (...p) => part(p) <= Math.min(...rabbitParts.map(f=>f(p)))
  set('rabbit',smin(.06,rabbitHead,rabbitEars,rabbitCheeks),[
    ...[-1,1].map(s=>band('#EFC0D2',(x,y,z)=>z>.02&&rabbitNearest(rabbitEars)(x,y,z)&&
      Math.hypot((x-s*.25+s*(y-.62)*.14)/.075,(y-.66)/.34)<1)),
    band('#F4ECEF',rabbitNearest(rabbitCheeks)),
    band('#F0A3BC',(x,y,z)=>z>.4&&Math.hypot(x/.07,(y+.33)/.045)<1),
    band('#FFFFFF',(x,y,z)=>z>.4&&rabbitNearest(rabbitHead)(x,y,z)&&Math.abs(x)<.065&&Math.abs(x)>.008&&y<-.55&&y>-.65),
    band('#E8A6BE',(x,y,z)=>z>.3&&rabbitNearest(rabbitHead)(x,y,z)&&Math.hypot((Math.abs(x)-.42)/.09,(y+.38)/.06)<1)],
    [[-.22,-.16],[.22,-.16]],.6)
  // A round penguin: a white heart-shaped face and belly, an orange beak and feet, and little flippers.
  const penguinBody = oval([0,-.06,0],[.64,.82,.58])
  const penguinFlippers = union(...[-1,1].map(s=>oval([s*.62,-.18,-.02],[.13,.36,.14],s*20)))
  const penguinBeak = roundCone([0,.16,.5],[0,.08,.76],.12,.035)
  const penguinFeet = union(...[-1,1].map(s=>oval([s*.24,-.86,.24],[.19,.08,.2])))
  const penguinParts = [penguinBody,penguinFlippers,penguinBeak,penguinFeet]
  const penguinNearest = part => (...p) => part(p) <= Math.min(...penguinParts.map(f=>f(p)))
  set('penguin',smin(.04,penguinBody,penguinFlippers,penguinBeak,penguinFeet),[
    band('#F4F1E8',(x,y,z)=>z>.08&&penguinNearest(penguinBody)(x,y,z)&&
      (Math.hypot((Math.abs(x)-.19)/.25,(y-.32)/.27)<1||Math.hypot(x/.45,(y+.3)/.5)<1)),
    band('#F2A6A0',(x,y,z)=>z>.3&&penguinNearest(penguinBody)(x,y,z)&&Math.hypot((Math.abs(x)-.38)/.08,(y-.12)/.05)<1),
    band('#E8993A',(x,y,z)=>penguinNearest(penguinBeak)(x,y,z)||penguinNearest(penguinFeet)(x,y,z))],
    [[-.19,.33],[.19,.33]],.54)
  const axolotlHead = oval([0,-.02,0],[.71,.53,.46])
  const gills = [], frills = []
  for (const s of [-1,1]) {
    for (const [a,b] of [[[.52,.22],[.91,.62]],[[.64,.03],[1.1,.12]],[[.55,-.2],[.93,-.52]]]) {
      const dx=b[0]-a[0], dy=b[1]-a[1], length=Math.hypot(dx,dy)
      gills.push(capsule([s*a[0],a[1],-.06],[s*b[0],b[1],-.06],.065))
      for (const t of [.4,.7]) for (const side of [-1,1]) {
        const x=a[0]+dx*t, y=a[1]+dy*t
        frills.push(capsule([s*x,y,-.06],
          [s*(x+.07*dx/length-side*.13*dy/length),y+.07*dy/length+side*.13*dx/length,-.06],.045))
      }
    }
  }
  const axolotlGills = union(...gills), axolotlFrills = union(...frills)
  const axolotlParts = [axolotlHead,axolotlGills,axolotlFrills]
  const axolotlNearest = part=>(...p)=>part(p)<=Math.min(...axolotlParts.map(f=>f(p)))
  set('axolotl',smin(.035,...axolotlParts),[
    band('#D46C91',axolotlNearest(axolotlGills)),
    band('#E78FA9',axolotlNearest(axolotlFrills)),
    band('#E98AA5',(x,y,z)=>z>.2&&axolotlNearest(axolotlHead)(x,y,z)&&Math.hypot((Math.abs(x)-.39)/.085,(y+.13)/.045)<1),
    band('#6A3C4C',(x,y,z)=>z>.3&&axolotlNearest(axolotlHead)(x,y,z)&&Math.abs(x)<.14&&Math.abs(y+.27-1.6*x*x)<.014)],
    [[-.24,.08],[.24,.08]],.7)
  const mouseHead = oval([0,-.15,0],[.63,.58,.47])
  const mouseEars = union(...[-1,1].map(s=>oval([s*.52,.47,-.05],[.34,.35,.17])))
  const mouseMuzzle = oval([0,-.37,.38],[.24,.19,.22])
  const mouseNose = oval([0,-.31,.6],[.085,.065,.07])
  const mouseParts = [mouseHead,mouseEars,mouseMuzzle,mouseNose]
  const mouseNearest = part=>(...p)=>part(p)<=Math.min(...mouseParts.map(f=>f(p)))
  set('mouse',smin(.045,...mouseParts),[
    band('#E6AEB6',(x,y,z)=>z>.035&&mouseNearest(mouseEars)(x,y,z)&&Math.hypot((Math.abs(x)-.52)/.235,(y-.49)/.245)<1),
    band('#E6DDD4',mouseNearest(mouseMuzzle)),
    band('#D78F9E',mouseNearest(mouseNose))],[[-.23,.04],[.23,.04]],.68)
}
