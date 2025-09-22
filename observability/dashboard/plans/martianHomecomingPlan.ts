import type { PlanDefinition } from './types';

// Mars noir: a stranded robot turns dust into delta‑v, writing letters to a lost wife back on Earth.
export const martianHomecomingPlan: PlanDefinition = {
  name: 'Homecoming',
  description:
    "A stranded robot on Mars rebuilds a ship from dust, fuel, and resolve to get back to Earth and his long‑lost wife. Letters to home punctuate a dark, hopeful sci‑fi heist against physics.",
  groups: [
    {
      id: 'P',
      title: 'Survival & Mission Plotting',
      description: 'Hard limits, map the crater, choose a way home, and outline the letter cadence.',
    },
    {
      id: 'B',
      title: 'Resource Extraction & Spaceframe Build',
      description: 'Turn Mars into parts: fuel, hull, avionics, comms, and guidance.',
    },
    {
      id: 'E',
      title: 'Trials & Validation',
      description: 'Burn, freeze, shake, and prove the stack can survive the red planet and the void.',
    },
    {
      id: 'D',
      title: 'Departure & Earth Return',
      description: 'Pad ops, ascent, cruise, entry, reunion.',
    },
  ],
  items: [
    // -----------------------------
    // Planning (P)
    // -----------------------------
    { id: 'PA1', group: 'P', sector: 'PLANNING', depends_on: [], estimate_ms: 23299, tps_min: 60, tps_max: 105, work_desc: 'Wake diagnostics; inventory power, mass, and time. Survival triage.' },
    { id: 'PA2', group: 'P', sector: 'PLANNING', depends_on: [], estimate_ms: 26827, tps_min: 58, tps_max: 96, work_desc: 'Crater survey: hazards, ice hints, basalt beds, and salvage beacons.' },
    { id: 'PA3', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 28209, tps_min: 43, tps_max: 97, work_desc: 'Define return objective, delta‑v budget, and success criteria.' },
    { id: 'PA4', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 15584, tps_min: 73, tps_max: 114, work_desc: 'Risk register: dust storms, radiation, actuator wear, memory decay.' },
    { id: 'PA5', group: 'P', sector: 'PLANNING', depends_on: ['PA2'], estimate_ms: 21093, tps_min: 51, tps_max: 114, work_desc: 'Resource map: ice veins, hematite, perchlorates, derelict landers.' },
    { id: 'PA6', group: 'P', sector: 'PLANNING', depends_on: ['PA3'], estimate_ms: 12573, tps_min: 66, tps_max: 110, work_desc: 'Fuel path: choose methalox via Sabatier; size reactors and power.' },
    { id: 'PA7', group: 'P', sector: 'PLANNING', depends_on: ['PA5'], estimate_ms: 28435, tps_min: 68, tps_max: 104, work_desc: 'Fabrication roadmap; BOM from in‑situ resources + salvage.' },
    { id: 'PA8', group: 'P', sector: 'PLANNING', depends_on: ['PA6'], estimate_ms: 11416, tps_min: 79, tps_max: 101, work_desc: 'Trajectory sketch; launch windows and corridor constraints.' },

    { id: 'PB1', group: 'P', sector: 'PLANNING', depends_on: ['PA2'], estimate_ms: 11967, tps_min: 64, tps_max: 100, work_desc: 'Hab & dust seals; shelter for long runs and letter writing.' },
    { id: 'PB2', group: 'P', sector: 'PLANNING', depends_on: ['PA5'], estimate_ms: 29614, tps_min: 69, tps_max: 118, work_desc: 'Ice mining plan; desalinate and neutralize perchlorates.' },
    { id: 'PB3', group: 'P', sector: 'PLANNING', depends_on: ['PA4'], estimate_ms: 10133, tps_min: 61, tps_max: 101, work_desc: 'Thermal survival plan: cold‑soak nights, radiator and heaters.' },
    { id: 'PB4', group: 'P', sector: 'PLANNING', depends_on: ['PA5'], estimate_ms: 25404, tps_min: 60, tps_max: 80, work_desc: 'Regolith to parts: kiln, fiber, sinter, and cast workcells.' },
    { id: 'PB5', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 18594, tps_min: 56, tps_max: 99, work_desc: 'Salvage rules: what to cannibalize; preserve heritage sites.' },
    { id: 'PB6', group: 'P', sector: 'PLANNING', depends_on: ['PB1'], estimate_ms: 17059, tps_min: 57, tps_max: 84, work_desc: 'Comms stack options: UHF relay, X‑band, optical laser.' },
    { id: 'PB7', group: 'P', sector: 'PLANNING', depends_on: ['PA7'], estimate_ms: 25425, tps_min: 40, tps_max: 110, work_desc: 'Material assays; update regolith composition model.' },
    { id: 'PB8', group: 'P', sector: 'PLANNING', depends_on: ['PA7'], estimate_ms: 11489, tps_min: 73, tps_max: 96, work_desc: 'Tooling list; printable jigs; contingency spares.' },

    { id: 'PC1', group: 'P', sector: 'PLANNING', depends_on: ['PA3', 'PB5'], estimate_ms: 23370, tps_min: 51, tps_max: 88, work_desc: 'Letter beam protocol v0; encryption & error correction.' },
    { id: 'PC2', group: 'P', sector: 'PLANNING', depends_on: ['PC1'], estimate_ms: 18765, tps_min: 80, tps_max: 96, work_desc: 'Storm mode triggers; safe‑kneel & blackout behaviors.' },
    { id: 'PC3', group: 'P', sector: 'PLANNING', depends_on: ['PA8', 'PC1'], estimate_ms: 18068, tps_min: 71, tps_max: 119, work_desc: 'Ephemeris and star catalog ingestion plan.' },
    { id: 'PC4', group: 'P', sector: 'PLANNING', depends_on: ['PB6', 'PC2'], estimate_ms: 22874, tps_min: 62, tps_max: 87, work_desc: 'Noir letter pack: voice, cadence, checksum rituals.' },

    { id: 'PD1', group: 'P', sector: 'PLANNING', depends_on: ['PB2', 'PA8'], estimate_ms: 23177, tps_min: 73, tps_max: 115, work_desc: 'Power plan: solar expansion, MPPT, battery refurb.' },
    { id: 'PD2', group: 'P', sector: 'PLANNING', depends_on: ['PB3', 'PA4'], estimate_ms: 11442, tps_min: 51, tps_max: 88, work_desc: 'Thermal plan: radiators, insulation, cryo losses.' },
    { id: 'PD3', group: 'P', sector: 'PLANNING', depends_on: ['PD1', 'PD2'], estimate_ms: 19727, tps_min: 47, tps_max: 114, work_desc: 'Critical path; fan‑in gates; go/no‑go criteria.' },
    { id: 'PD4', group: 'P', sector: 'PLANNING', depends_on: ['PD3'], estimate_ms: 22660, tps_min: 66, tps_max: 92, work_desc: 'Operator (me) SOPs; checklists for lonely nights.' },

    // -----------------------------
    // Build (B)
    // -----------------------------
    { id: 'BA1', group: 'B', sector: 'BUILD', depends_on: ['PD2', 'PA2'], estimate_ms: 19465, tps_min: 45, tps_max: 107, work_desc: 'CO₂ capture: compressor, filters, frost trap.' },
    { id: 'BA2', group: 'B', sector: 'BUILD', depends_on: ['PD2', 'PB2'], estimate_ms: 27324, tps_min: 69, tps_max: 113, work_desc: 'Electrolyzer assembly; O₂/H₂ split under dust‑proof enclosures.' },
    { id: 'BA3', group: 'B', sector: 'BUILD', depends_on: ['PB2'], estimate_ms: 19739, tps_min: 59, tps_max: 117, work_desc: 'Ice mining rig; drill, melt, desalinate; perchlorate scrub.' },
    { id: 'BA4', group: 'B', sector: 'BUILD', depends_on: ['BA1', 'BA2'], estimate_ms: 23276, tps_min: 60, tps_max: 116, work_desc: 'Sabatier reactor; nickel catalyst micro‑channels; heat coupling.' },
    { id: 'BA5', group: 'B', sector: 'BUILD', depends_on: ['BA4'], estimate_ms: 21319, tps_min: 73, tps_max: 111, work_desc: 'Cryo plant; CH₄ liquefaction and O₂ condensation; dewars.' },
    { id: 'BA6', group: 'B', sector: 'BUILD', depends_on: ['PB4'], estimate_ms: 23593, tps_min: 49, tps_max: 99, work_desc: 'Microwave kiln for regolith sintering; brick and tile line.' },
    { id: 'BA7', group: 'B', sector: 'BUILD', depends_on: ['BA6'], estimate_ms: 13986, tps_min: 56, tps_max: 116, work_desc: 'Basalt fiber extruder; tow winding and spooler.' },
    { id: 'BA8', group: 'B', sector: 'BUILD', depends_on: ['PB7', 'BA6'], estimate_ms: 20309, tps_min: 79, tps_max: 88, work_desc: 'Molten‑oxide electrolysis cell; Fe/Al ingots from dust.' },
    { id: 'BA9', group: 'B', sector: 'BUILD', depends_on: ['BA7', 'BA6'], estimate_ms: 28000, tps_min: 48, tps_max: 80, work_desc: '3D print pressure hull sections with basalt‑regolith composite.' },
    { id: 'BA10', group: 'B', sector: 'BUILD', depends_on: ['BA8'], estimate_ms: 20639, tps_min: 58, tps_max: 108, work_desc: 'Tankage spin‑forming; welds; proof fixtures.' },
    { id: 'BA11', group: 'B', sector: 'BUILD', depends_on: ['BA6'], estimate_ms: 21806, tps_min: 59, tps_max: 107, work_desc: 'Seals & gaskets from sulfur polymer; seat tests.' },
    { id: 'BA12', group: 'B', sector: 'BUILD', depends_on: ['PB5'], estimate_ms: 28342, tps_min: 47, tps_max: 114, work_desc: 'Electronics salvage: power rails, MCUs, RF front‑ends.' },
    { id: 'BA13', group: 'B', sector: 'BUILD', depends_on: ['BA12'], estimate_ms: 15955, tps_min: 60, tps_max: 104, work_desc: 'Star tracker build; baffling; dark‑tent alignment jig.' },
    { id: 'BA14', group: 'B', sector: 'BUILD', depends_on: ['BA13'], estimate_ms: 19700, tps_min: 61, tps_max: 110, work_desc: 'GN&C stack; IMU + sun sensor fusion.' },
    { id: 'BA15', group: 'B', sector: 'BUILD', depends_on: ['PB6', 'BA12'], estimate_ms: 26653, tps_min: 58, tps_max: 102, work_desc: 'Deep‑space comms: X‑band dish & laser head on gimbal.' },
    { id: 'BA16', group: 'B', sector: 'BUILD', depends_on: ['BA15'], estimate_ms: 16524, tps_min: 62, tps_max: 105, work_desc: 'Antenna lens heaters and dust sweepers.' },
    { id: 'BA17', group: 'B', sector: 'BUILD', depends_on: ['BA11', 'BA9'], estimate_ms: 19406, tps_min: 60, tps_max: 93, work_desc: 'Simple airlock; pressure boundary integration.' },
    { id: 'BA18', group: 'B', sector: 'BUILD', depends_on: ['PD2', 'BA9'], estimate_ms: 15395, tps_min: 53, tps_max: 86, work_desc: 'Thermal & compute radiator pack; loop plumbing.' },
    { id: 'BA19', group: 'B', sector: 'BUILD', depends_on: ['BA4', 'BA8'], estimate_ms: 16823, tps_min: 62, tps_max: 115, work_desc: 'Main engine: injector, regen‑cooled nozzle, igniter.' },
    { id: 'BA20', group: 'B', sector: 'BUILD', depends_on: ['BA10', 'BA19'], estimate_ms: 24346, tps_min: 54, tps_max: 114, work_desc: 'Pressure‑fed feed system; high‑pressure plumbing and valves.' },
    { id: 'BA21', group: 'B', sector: 'BUILD', depends_on: ['BA19'], estimate_ms: 10013, tps_min: 64, tps_max: 87, work_desc: 'RCS thrusters and cold‑gas reserve.' },
    { id: 'BA22', group: 'B', sector: 'BUILD', depends_on: ['BA6', 'BA8'], estimate_ms: 24155, tps_min: 61, tps_max: 82, work_desc: 'Landing legs with crush‑core footpads.' },
    { id: 'BA23', group: 'B', sector: 'BUILD', depends_on: ['BA14'], estimate_ms: 28169, tps_min: 72, tps_max: 117, work_desc: 'Autopilot; entry/landing hazard map logic.' },
    { id: 'BA24', group: 'B', sector: 'BUILD', depends_on: ['BA12', 'PD1'], estimate_ms: 16051, tps_min: 44, tps_max: 107, work_desc: 'Battery refurb and solar bus controller.' },
    { id: 'BA25', group: 'B', sector: 'BUILD', depends_on: ['BA24'], estimate_ms: 27421, tps_min: 47, tps_max: 110, work_desc: 'Solar field expansion; MPPT micro‑inverters.' },
    { id: 'BA26', group: 'B', sector: 'BUILD', depends_on: ['PB1', 'PC4'], estimate_ms: 24524, tps_min: 60, tps_max: 120, work_desc: 'Black‑box logger + engraved letter plates (“Noir Log”).' },
    { id: 'BA27', group: 'B', sector: 'BUILD', depends_on: ['BA5'], estimate_ms: 28404, tps_min: 75, tps_max: 110, work_desc: 'Cryo insulation: multilayer blankets and silica aerogel.' },
    { id: 'BA28', group: 'B', sector: 'BUILD', depends_on: ['BA5', 'BA20'], estimate_ms: 15677, tps_min: 53, tps_max: 80, work_desc: 'Fuel/oxidizer plumbing; checks; blow‑down logic.' },
    { id: 'BA29', group: 'B', sector: 'BUILD', depends_on: ['BA12', 'BA14'], estimate_ms: 19282, tps_min: 43, tps_max: 92, work_desc: 'Flight computer; redundant buses; rad shielding (regolith tile).' },
    { id: 'BA30', group: 'B', sector: 'BUILD', depends_on: ['BA9', 'BA26'], estimate_ms: 21889, tps_min: 78, tps_max: 88, work_desc: 'Hull “mailbox” vault for physical letters.' },
    { id: 'BA31', group: 'B', sector: 'BUILD', depends_on: ['BA6', 'BA18'], estimate_ms: 29659, tps_min: 46, tps_max: 92, work_desc: 'Heat shield: phenolic char + aerogel tile stack.' },
    { id: 'BA32', group: 'B', sector: 'BUILD', depends_on: ['BA29'], estimate_ms: 10280, tps_min: 80, tps_max: 108, work_desc: 'Fairing/cover ejectors; spring‑bolt actuation; safing.' },
    { id: 'BA33', group: 'B', sector: 'BUILD', depends_on: ['BA15'], estimate_ms: 12686, tps_min: 65, tps_max: 110, work_desc: 'S‑band proximity transceiver for Earthside beacon lock.' },
    { id: 'BA34', group: 'B', sector: 'BUILD', depends_on: ['BA13', 'PC3'], estimate_ms: 14386, tps_min: 51, tps_max: 85, work_desc: 'Ephemeris uploader and star catalog seed.' },
    { id: 'BA35', group: 'B', sector: 'BUILD', depends_on: ['BA9'], estimate_ms: 16487, tps_min: 46, tps_max: 111, work_desc: 'Mission patch acid‑etched on hull: “FIND AURORA”.' },
    { id: 'BA36', group: 'B', sector: 'BUILD', depends_on: ['PC4', 'BA29'], estimate_ms: 14974, tps_min: 67, tps_max: 113, work_desc: 'Encrypted letter vault; checksums; restore keys.' },
    { id: 'BA37', group: 'B', sector: 'BUILD', depends_on: ['PC2', 'BA22'], estimate_ms: 19167, tps_min: 79, tps_max: 109, work_desc: 'Autonomous storm mode: anchors, tent, low‑profile posture.' },
    { id: 'BA38', group: 'B', sector: 'BUILD', depends_on: ['BA6'], estimate_ms: 29207, tps_min: 58, tps_max: 113, work_desc: 'Ore hauler kit: wheels/tracks for dune crossings.' },
    { id: 'BA39', group: 'B', sector: 'BUILD', depends_on: ['BA3'], estimate_ms: 13978, tps_min: 79, tps_max: 97, work_desc: 'Water purification & storage; perchlorate neutralizer plant.' },
    { id: 'BA40', group: 'B', sector: 'BUILD', depends_on: ['BA4'], estimate_ms: 29140, tps_min: 58, tps_max: 119, work_desc: 'Catalyst regeneration line; spare cartridges.' },
    { id: 'BA41', group: 'B', sector: 'BUILD', depends_on: ['PA8', 'BA34', 'BA29'], estimate_ms: 24838, tps_min: 57, tps_max: 119, work_desc: 'Trajectory & launch‑window solver; corridor constraints.' },

    // -----------------------------
    // Evaluation (E)
    // -----------------------------
    { id: 'EA1', group: 'E', sector: 'EVAL', depends_on: ['BA24', 'BA18'], estimate_ms: 26215, tps_min: 71, tps_max: 86, work_desc: 'Power & thermal endurance across a Martian night.' },
    { id: 'EA2', group: 'E', sector: 'EVAL', depends_on: ['BA2'], estimate_ms: 18471, tps_min: 62, tps_max: 85, work_desc: 'Electrolyzer continuous run; O₂ purity & membrane health.' },
    { id: 'EA3', group: 'E', sector: 'EVAL', depends_on: ['BA4'], estimate_ms: 23498, tps_min: 72, tps_max: 102, work_desc: 'Sabatier yield curve; catalyst life modeling.' },
    { id: 'EA4', group: 'E', sector: 'EVAL', depends_on: ['BA5', 'BA27'], estimate_ms: 28979, tps_min: 62, tps_max: 118, work_desc: 'Cryo boil‑off rate; insulation performance.' },
    { id: 'EA5', group: 'E', sector: 'EVAL', depends_on: ['BA10', 'BA28'], estimate_ms: 11242, tps_min: 62, tps_max: 87, work_desc: 'Tank pressure proof & plumbing leak checks.' },
    { id: 'EA6', group: 'E', sector: 'EVAL', depends_on: ['BA19', 'BA28'], estimate_ms: 22492, tps_min: 40, tps_max: 116, work_desc: 'Engine static fire (short) with instrumentation.' },
    { id: 'EA7', group: 'E', sector: 'EVAL', depends_on: ['EA6'], estimate_ms: 16315, tps_min: 73, tps_max: 104, work_desc: 'Engine static fire (long); thermal margins & ignition repeatability.' },
    { id: 'EA8', group: 'E', sector: 'EVAL', depends_on: ['BA21', 'BA29'], estimate_ms: 19732, tps_min: 77, tps_max: 84, work_desc: 'RCS pulsing; attitude hold & limit cycles.' },
    { id: 'EA9', group: 'E', sector: 'EVAL', depends_on: ['BA29', 'BA34'], estimate_ms: 29890, tps_min: 41, tps_max: 109, work_desc: 'Avionics HIL sim; star‑tracker dark‑tent validation.' },
    { id: 'EA10', group: 'E', sector: 'EVAL', depends_on: ['BA15', 'BA36'], estimate_ms: 20844, tps_min: 74, tps_max: 102, work_desc: 'Earth link budget; letter send dry‑run with checksum audit.' },
    { id: 'EA11', group: 'E', sector: 'EVAL', depends_on: ['BA37'], estimate_ms: 19107, tps_min: 57, tps_max: 100, work_desc: 'Dust storm survival drill; anchor and ride‑out.' },
    { id: 'EA12', group: 'E', sector: 'EVAL', depends_on: ['BA17'], estimate_ms: 14879, tps_min: 78, tps_max: 86, work_desc: 'Hull pressure integrity; airlock seal tests.' },
    { id: 'EA13', group: 'E', sector: 'EVAL', depends_on: ['BA31'], estimate_ms: 14035, tps_min: 65, tps_max: 101, work_desc: 'Heat‑shield char & erosion under arc‑jet surrogate.' },
    { id: 'EA14', group: 'E', sector: 'EVAL', depends_on: ['BA41'], estimate_ms: 22197, tps_min: 47, tps_max: 94, work_desc: 'Trajectory solver validation vs ephemerides; corridor Monte Carlo.' },
    { id: 'EA15', group: 'E', sector: 'EVAL', depends_on: ['BA25'], estimate_ms: 18774, tps_min: 68, tps_max: 98, work_desc: 'Battery endurance; solar field recovery after dusting.' },
    { id: 'EA16', group: 'E', sector: 'EVAL', depends_on: ['BA41', 'BA13'], estimate_ms: 20167, tps_min: 52, tps_max: 114, work_desc: 'Navigation endurance; dead‑reckon vs star‑fix drift.' },
    { id: 'EA17', group: 'E', sector: 'EVAL', depends_on: ['BA26', 'BA29'], estimate_ms: 22919, tps_min: 48, tps_max: 117, work_desc: 'Black‑box integrity & fault injection; letter log immutability.' },
    { id: 'EA18', group: 'E', sector: 'EVAL', depends_on: ['BA9', 'BA18'], estimate_ms: 28678, tps_min: 40, tps_max: 104, work_desc: 'Hull thermal maps; radiator loop performance under sun + wind.' },
    { id: 'EA19', group: 'E', sector: 'EVAL', depends_on: ['BA10'], estimate_ms: 20189, tps_min: 45, tps_max: 117, work_desc: 'Tank slosh and baffle effectiveness; vibration survey.' },
    { id: 'EA20', group: 'E', sector: 'EVAL', depends_on: ['BA29'], estimate_ms: 12616, tps_min: 58, tps_max: 86, work_desc: 'EMI/grounding noise rejection and bus resets.' },

    // -----------------------------
    // Departure & Return (D)
    // -----------------------------
    { id: 'DA1', group: 'D', sector: 'DEPLOY', depends_on: ['EA1', 'EA5'], estimate_ms: 20688, tps_min: 56, tps_max: 85, work_desc: 'Stack vehicle on pad; final bolts torqued and tagged.' },
    { id: 'DA2', group: 'D', sector: 'DEPLOY', depends_on: ['EA4', 'EA5'], estimate_ms: 14942, tps_min: 43, tps_max: 106, work_desc: 'Load cryo; chilldown; umbilicals checkout.' },
    { id: 'DB1', group: 'D', sector: 'DEPLOY', depends_on: ['EA10', 'EA17'], estimate_ms: 10172, tps_min: 57, tps_max: 109, work_desc: 'Final letter to AURORA; engrave plate; transmit checksum.' },
    { id: 'DA3', group: 'D', sector: 'DEPLOY', depends_on: ['EA14', 'DA1'], estimate_ms: 14373, tps_min: 63, tps_max: 83, work_desc: 'Go/No‑Go poll; commit to ignition window.' },
    { id: 'DB2', group: 'D', sector: 'DEPLOY', depends_on: ['DA3', 'DA2'], estimate_ms: 24112, tps_min: 58, tps_max: 116, work_desc: 'Ignition and liftoff; rise above the rust.' },
    { id: 'DC1', group: 'D', sector: 'DEPLOY', depends_on: ['DB2', 'EA14'], estimate_ms: 16742, tps_min: 57, tps_max: 91, work_desc: 'Ascent guidance; pitch program and corridor tracking.' },
    { id: 'DC2', group: 'D', sector: 'DEPLOY', depends_on: ['DC1'], estimate_ms: 20515, tps_min: 57, tps_max: 117, work_desc: 'Mass check; pressure‑fed continuity; continue burn profile.' },
    { id: 'DD1', group: 'D', sector: 'DEPLOY', depends_on: ['DC2', 'BA41'], estimate_ms: 15097, tps_min: 80, tps_max: 96, work_desc: 'Trans‑Earth injection burn; solver‑guided.' },
    { id: 'DD2', group: 'D', sector: 'DEPLOY', depends_on: ['EA10', 'DD1'], estimate_ms: 17212, tps_min: 62, tps_max: 119, work_desc: 'Deep‑space comms check; letters beamed nightly.' },
    { id: 'DE1', group: 'D', sector: 'DEPLOY', depends_on: ['DD1', 'EA9'], estimate_ms: 13787, tps_min: 44, tps_max: 114, work_desc: 'Mid‑course correction #1; star‑tracker alignment.' },
    { id: 'DE2', group: 'D', sector: 'DEPLOY', depends_on: ['DE1'], estimate_ms: 15846, tps_min: 63, tps_max: 89, work_desc: 'Mid‑course correction #2; nav sanity checks.' },
    { id: 'DF1', group: 'D', sector: 'DEPLOY', depends_on: ['BA31', 'EA14'], estimate_ms: 24726, tps_min: 75, tps_max: 106, work_desc: 'Entry interface; bank through corridor per guidance.' },
    { id: 'DF2', group: 'D', sector: 'DEPLOY', depends_on: ['DF1', 'EA8'], estimate_ms: 25899, tps_min: 52, tps_max: 82, work_desc: 'Retro burn and RCS trim; blackout countdown.' },
    { id: 'DG1', group: 'D', sector: 'DEPLOY', depends_on: ['DF2', 'EA17'], estimate_ms: 12180, tps_min: 80, tps_max: 80, work_desc: 'Black‑box final dump; safe‑landing routine.' },
    { id: 'DH1', group: 'D', sector: 'DEPLOY', depends_on: ['DG1', 'BA33'], estimate_ms: 23210, tps_min: 77, tps_max: 91, work_desc: 'Earth beacon acquisition; S‑band handshake.' },
    { id: 'DI1', group: 'D', sector: 'DEPLOY', depends_on: ['DG1'], estimate_ms: 25320, tps_min: 76, tps_max: 120, work_desc: 'Touchdown and cooling; O₂ purge and safing.' },
    { id: 'DJ1', group: 'D', sector: 'DEPLOY', depends_on: ['DH1'], estimate_ms: 20319, tps_min: 43, tps_max: 96, work_desc: 'Unseal vault; find AURORA or leave a trail she can follow.' },
    { id: 'DK1', group: 'D', sector: 'DEPLOY', depends_on: ['DJ1'], estimate_ms: 24914, tps_min: 41, tps_max: 97, work_desc: 'Post‑flight letter: “I made it.”' },
    { id: 'DL1', group: 'D', sector: 'DEPLOY', depends_on: ['DK1', 'DG1'], estimate_ms: 24249, tps_min: 57, tps_max: 109, work_desc: 'Mission postmortem; archive letters, logs, and lessons.' },
  ],
};

export default martianHomecomingPlan;
