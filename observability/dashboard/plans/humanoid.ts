import type { PlanDefinition } from './types';

// Humanoid plan: more items, more parallelism, higher tps variance
export const humanoidPlan: PlanDefinition = {
  name: 'Humanoid',
  description: 'Building a humanoid rock-hauling robot. Get the robot to the top of the mountain and back.',
  groups: [
    {
      id: 'P',
      title: 'Mission Planning',
      description: 'Define mission goals, constraints, terrain routes, budget, and safety architecture for the ascent.',
    },
    {
      id: 'B',
      title: 'Mechanical & Power Build',
      description: 'Exoskeleton fabrication, actuation, power systems, and sensors required to haul rocks uphill.',
    },
    {
      id: 'E',
      title: 'Evaluation & Field Tests',
      description: 'Bench and field validation, calibration, and timed haul cycles under load.',
    },
    {
      id: 'D',
      title: 'Deployment & Operations',
      description: 'Productionization, staging at the mountain base, and operational readiness for repeated hauls.',
    },
  ],
  items: [
    // -----------------------------
    // Planning
    // -----------------------------
    { id: 'PA1', group: 'P', sector: 'PLANNING', depends_on: [], estimate_ms: 17653, tps_min: 54, tps_max: 93, work_desc: 'Define mission goals and safety constraints for rock-hauling humanoid.' },
    { id: 'PA2', group: 'P', sector: 'PLANNING', depends_on: [], estimate_ms: 21949, tps_min: 47, tps_max: 105, work_desc: 'Draft system architecture: gait plan, payload target, torque budget.' },
    { id: 'PB1', group: 'P', sector: 'PLANNING', depends_on: [], estimate_ms: 21030, tps_min: 56, tps_max: 118, work_desc: 'Analyze mountain terrain; compile routes and incline profiles dataset.' },

    { id: 'PA3', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 27475, tps_min: 64, tps_max: 96, work_desc: 'Build risk register: avalanche, rockfall, battery thermal runaway.' },
    { id: 'PA4', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 19034, tps_min: 70, tps_max: 113, work_desc: 'Define success metrics & telemetry KPIs (time-to-summit, Wh/m, fault rate).' },
    { id: 'PA5', group: 'P', sector: 'PLANNING', depends_on: ['PB1'], estimate_ms: 17207, tps_min: 64, tps_max: 110, work_desc: 'Permits & land access: coordinate with park rangers and trail stewards.' },
    { id: 'PB2', group: 'P', sector: 'PLANNING', depends_on: ['PB1'], estimate_ms: 23239, tps_min: 41, tps_max: 109, work_desc: 'Create synthetic rock-shape/weight dataset via photogrammetry.' },
    { id: 'PB3', group: 'P', sector: 'PLANNING', depends_on: ['PA2'], estimate_ms: 22745, tps_min: 62, tps_max: 102, work_desc: 'BOM & budget; flag long-lead items and alternates.' },
    { id: 'PC1', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 27284, tps_min: 40, tps_max: 83, work_desc: 'Define HRI & E‑stop protocols; operator roles and comms.' },
    { id: 'PC2', group: 'P', sector: 'PLANNING', depends_on: ['PA2'], estimate_ms: 23934, tps_min: 41, tps_max: 112, work_desc: 'Gait and terrain sim parameter sweep plan.' },
    { id: 'PD1', group: 'P', sector: 'PLANNING', depends_on: ['PA1'], estimate_ms: 18658, tps_min: 45, tps_max: 82, work_desc: 'Comms plan: base-camp radio net + LTE fallback.' },
    { id: 'PD2', group: 'P', sector: 'PLANNING', depends_on: ['PA2'], estimate_ms: 24768, tps_min: 63, tps_max: 106, work_desc: 'Cold‑weather ops plan: battery warmers, low-temp grease, seal choices.' },
    { id: 'PE1', group: 'P', sector: 'PLANNING', depends_on: ['PC1'], estimate_ms: 28811, tps_min: 53, tps_max: 106, work_desc: 'Operator training script & safety drills.' },
    { id: 'PE2', group: 'P', sector: 'PLANNING', depends_on: ['PA2', 'PB1'], estimate_ms: 20655, tps_min: 67, tps_max: 108, work_desc: 'Test matrix: ascent/descent scenarios and acceptance gates.' },
    { id: 'PF1', group: 'P', sector: 'PLANNING', depends_on: ['PB3'], estimate_ms: 24040, tps_min: 74, tps_max: 97, work_desc: 'Spare parts & field kit list with quantities.' },
    { id: 'PF2', group: 'P', sector: 'PLANNING', depends_on: ['PD1'], estimate_ms: 17851, tps_min: 61, tps_max: 82, work_desc: 'Narrative beats for docu-log; media checklist (for fun).' },

    // -----------------------------
    // Build branches
    // -----------------------------
    { id: 'BA1', group: 'B', sector: 'BUILD', depends_on: ['PA1'], estimate_ms: 26318, tps_min: 46, tps_max: 118, work_desc: 'Fabricate exoskeleton frame; assemble hip/shoulder joint modules.' },
    { id: 'BA2', group: 'B', sector: 'BUILD', depends_on: ['BA1'], estimate_ms: 20017, tps_min: 60, tps_max: 106, work_desc: 'Install actuators and motor drivers; verify stall torque on rig.' },
    { id: 'BB1', group: 'B', sector: 'BUILD', depends_on: ['PB1'], estimate_ms: 26913, tps_min: 60, tps_max: 83, work_desc: 'Prototype power pack and thermal spreaders for high-load climbs.' },
    { id: 'BB2', group: 'B', sector: 'BUILD', depends_on: ['BB1'], estimate_ms: 20458, tps_min: 57, tps_max: 86, work_desc: 'Implement power management firmware and brownout protections.' },
    { id: 'BC1', group: 'B', sector: 'BUILD', depends_on: ['PA2'], estimate_ms: 13732, tps_min: 52, tps_max: 84, work_desc: 'Integrate IMU, LiDAR, and foot force sensors; wire harness.' },

    { id: 'BA3', group: 'B', sector: 'BUILD', depends_on: ['PA2'], estimate_ms: 24002, tps_min: 72, tps_max: 100, work_desc: 'Design gripper and basket clamp for irregular rock shapes.' },
    { id: 'BA4', group: 'B', sector: 'BUILD', depends_on: ['BA1'], estimate_ms: 28106, tps_min: 68, tps_max: 81, work_desc: 'Assemble knee/ankle spring‑dampers; select bushings.' },
    { id: 'BA5', group: 'B', sector: 'BUILD', depends_on: ['BA1'], estimate_ms: 29462, tps_min: 46, tps_max: 88, work_desc: 'Carbon shrouds & seal kit for dust/water ingress.' },
    { id: 'BB3', group: 'B', sector: 'BUILD', depends_on: ['BB1'], estimate_ms: 28442, tps_min: 66, tps_max: 115, work_desc: 'Battery pack rev‑B; cold‑soak performance upgrades.' },
    { id: 'BB4', group: 'B', sector: 'BUILD', depends_on: ['BB1'], estimate_ms: 10395, tps_min: 64, tps_max: 102, work_desc: 'Integrate MPPT charger with foldable solar mats (field topping).' },
    { id: 'BC2', group: 'B', sector: 'BUILD', depends_on: ['BC1'], estimate_ms: 18294, tps_min: 68, tps_max: 118, work_desc: 'EMI shielding/grounding; cable routing & strain relief.' },
    { id: 'BC3', group: 'B', sector: 'BUILD', depends_on: ['BC1'], estimate_ms: 20711, tps_min: 73, tps_max: 86, work_desc: 'Toe force sensors redundant path; analog fallback.' },
    { id: 'BD1', group: 'B', sector: 'BUILD', depends_on: ['BA1'], estimate_ms: 14329, tps_min: 74, tps_max: 119, work_desc: 'Footpad design: cleat vs microspike; quick‑change soles.' },
    { id: 'BD2', group: 'B', sector: 'BUILD', depends_on: ['BA2'], estimate_ms: 21290, tps_min: 72, tps_max: 117, work_desc: 'Winch assist module for 45°+ pitches.' },
    { id: 'BE1', group: 'B', sector: 'BUILD', depends_on: ['BA2', 'BB2'], estimate_ms: 24071, tps_min: 40, tps_max: 117, work_desc: 'Thermal management: heatpipes & ducting around drivers.' },
    { id: 'BE2', group: 'B', sector: 'BUILD', depends_on: ['BC2'], estimate_ms: 25920, tps_min: 43, tps_max: 120, work_desc: 'Conformal coating & potting for electronics.' },
    { id: 'BF1', group: 'B', sector: 'BUILD', depends_on: ['PA2'], estimate_ms: 14601, tps_min: 59, tps_max: 80, work_desc: 'Gait controller v1 (ZMP/SLIP hybrid) with torque limits.' },
    { id: 'BF2', group: 'B', sector: 'BUILD', depends_on: ['BC1'], estimate_ms: 21318, tps_min: 75, tps_max: 85, work_desc: 'Sensor fusion & foot slip estimator (Kalman).' },
    { id: 'BG1', group: 'B', sector: 'BUILD', depends_on: ['BF1'], estimate_ms: 26241, tps_min: 80, tps_max: 120, work_desc: 'Fall detection + safe‑kneel routine.' },
    { id: 'BG2', group: 'B', sector: 'BUILD', depends_on: ['BA3'], estimate_ms: 16792, tps_min: 72, tps_max: 101, work_desc: 'Rock grasp strategy library (pinch, cradle, lash).' },
    { id: 'BH1', group: 'B', sector: 'BUILD', depends_on: ['PB2', 'BC1'], estimate_ms: 18737, tps_min: 79, tps_max: 104, work_desc: 'Vision terrain classifier (scree/slab/talus).' },
    { id: 'BH2', group: 'B', sector: 'BUILD', depends_on: ['BC1'], estimate_ms: 22608, tps_min: 49, tps_max: 100, work_desc: 'LiDAR snow/ice reflectance compensation.' },
    { id: 'BI1', group: 'B', sector: 'BUILD', depends_on: ['PD1', 'BC1'], estimate_ms: 27082, tps_min: 79, tps_max: 84, work_desc: 'Comms stack: mesh radios + LTE fallback.' },
    { id: 'BJ1', group: 'B', sector: 'BUILD', depends_on: ['PC1', 'BI1'], estimate_ms: 15020, tps_min: 55, tps_max: 102, work_desc: 'Teleop dashboard with joystick mapping and overlays.' },
    { id: 'BK1', group: 'B', sector: 'BUILD', depends_on: ['PA4'], estimate_ms: 12831, tps_min: 78, tps_max: 86, work_desc: 'Black‑box logging: circular buffer + SD card.' },
    { id: 'BL1', group: 'B', sector: 'BUILD', depends_on: ['BG1'], estimate_ms: 16503, tps_min: 41, tps_max: 89, work_desc: 'Self‑righting routine & torque‑limited push‑up.' },
    { id: 'BM1', group: 'B', sector: 'BUILD', depends_on: ['BB2'], estimate_ms: 15506, tps_min: 72, tps_max: 91, work_desc: 'Battery hot‑swap latch & interlock.' },
    { id: 'BN1', group: 'B', sector: 'BUILD', depends_on: ['PD2', 'BB2'], estimate_ms: 12096, tps_min: 70, tps_max: 83, work_desc: 'Toe heaters & pack warmers control.' },
    { id: 'BO1', group: 'B', sector: 'BUILD', depends_on: ['BA3'], estimate_ms: 16313, tps_min: 57, tps_max: 102, work_desc: 'Payload basket quick‑release & weight sensor.' },
    { id: 'BP1', group: 'B', sector: 'BUILD', depends_on: ['BA3'], estimate_ms: 24482, tps_min: 53, tps_max: 97, work_desc: 'Arm/hand compliance tuning; underactuated fingers.' },
    { id: 'BQ1', group: 'B', sector: 'BUILD', depends_on: ['PB1', 'BH1'], estimate_ms: 12003, tps_min: 75, tps_max: 119, work_desc: 'Onboard map caching & route planner module.' },
    { id: 'BR1', group: 'B', sector: 'BUILD', depends_on: ['BC1', 'BF1'], estimate_ms: 16859, tps_min: 46, tps_max: 96, work_desc: 'Wind gust compensation using torso IMU.' },
    { id: 'BS1', group: 'B', sector: 'BUILD', depends_on: ['BF1', 'BE1'], estimate_ms: 18287, tps_min: 49, tps_max: 120, work_desc: 'Energy‑optimal gait planner (MPC).' },
    { id: 'BT1', group: 'B', sector: 'BUILD', depends_on: ['BB2'], estimate_ms: 24673, tps_min: 80, tps_max: 120, work_desc: 'Firmware watchdogs & brownout recovery.' },
    { id: 'BU1', group: 'B', sector: 'BUILD', depends_on: ['BK1'], estimate_ms: 13391, tps_min: 49, tps_max: 102, work_desc: 'Diagnostics LCD & beeper; field error codes.' },
    { id: 'BV1', group: 'B', sector: 'BUILD', depends_on: ['BE2'], estimate_ms: 20738, tps_min: 58, tps_max: 120, work_desc: 'Cable management tidy pass; tie-downs & labels.' },
    { id: 'BW1', group: 'B', sector: 'BUILD', depends_on: ['BA5', 'BE2'], estimate_ms: 24676, tps_min: 69, tps_max: 93, work_desc: 'Ingress test tweaks toward IP54.' },
    { id: 'BX1', group: 'B', sector: 'BUILD', depends_on: ['BA2'], estimate_ms: 15691, tps_min: 48, tps_max: 97, work_desc: 'Motor driver autotune under payload.' },
    { id: 'BY1', group: 'B', sector: 'BUILD', depends_on: ['PB1', 'BF1'], estimate_ms: 13462, tps_min: 68, tps_max: 105, work_desc: 'Footstep planner constraints generated from incline profiles.' },
    { id: 'BZ1', group: 'B', sector: 'BUILD', depends_on: ['BD2'], estimate_ms: 16292, tps_min: 52, tps_max: 87, work_desc: 'Emergency rope anchor points (no parachutes, sadly).' },

    // -----------------------------
    // Eval fan-in
    // -----------------------------
    { id: 'EA1', group: 'E', sector: 'EVAL', depends_on: ['BA2', 'BB2'], estimate_ms: 19789, tps_min: 75, tps_max: 92, work_desc: 'Bench test chassis + power under load; measure thermal margins.' },
    { id: 'EA2', group: 'E', sector: 'EVAL', depends_on: ['BC1'], estimate_ms: 22654, tps_min: 62, tps_max: 117, work_desc: 'Calibrate sensors; validate sensor fusion on incline jig.' },
    { id: 'EB1', group: 'E', sector: 'EVAL', depends_on: ['EA1'], estimate_ms: 24857, tps_min: 73, tps_max: 106, work_desc: 'Field simulate ascent on treadmill with weighted rock basket.' },

    { id: 'EA3', group: 'E', sector: 'EVAL', depends_on: ['BA3'], estimate_ms: 11179, tps_min: 44, tps_max: 101, work_desc: 'Run end‑to‑end timed haul cycles; tune gait vs. payload.' },
    { id: 'EA4', group: 'E', sector: 'EVAL', depends_on: ['BF1', 'EA1'], estimate_ms: 21947, tps_min: 43, tps_max: 119, work_desc: 'Gait stability on uneven rocks; perturbation test.' },
    { id: 'EB2', group: 'E', sector: 'EVAL', depends_on: ['BB2', 'BN1'], estimate_ms: 16406, tps_min: 75, tps_max: 89, work_desc: 'Battery endurance with 15 kg payload; cold‑soak −5°C.' },
    { id: 'EC1', group: 'E', sector: 'EVAL', depends_on: ['BI1'], estimate_ms: 29591, tps_min: 54, tps_max: 113, work_desc: 'RF link range & dropout characterization on trail.' },
    { id: 'EC2', group: 'E', sector: 'EVAL', depends_on: ['BJ1', 'BT1'], estimate_ms: 28336, tps_min: 75, tps_max: 112, work_desc: 'E‑stop latency under load; brake hold verification.' },
    { id: 'ED1', group: 'E', sector: 'EVAL', depends_on: ['BD1'], estimate_ms: 26790, tps_min: 50, tps_max: 100, work_desc: 'Sole/tread swap tests across scree, slab, and talus.' },
    { id: 'ED2', group: 'E', sector: 'EVAL', depends_on: ['BD2'], estimate_ms: 22425, tps_min: 44, tps_max: 113, work_desc: 'Winch assist regression on steep treadmill profile.' },
    { id: 'EE1', group: 'E', sector: 'EVAL', depends_on: ['BQ1'], estimate_ms: 11716, tps_min: 68, tps_max: 117, work_desc: 'Route planner A/B vs human guide timings.' },
    { id: 'EE2', group: 'E', sector: 'EVAL', depends_on: ['BE1'], estimate_ms: 12041, tps_min: 61, tps_max: 95, work_desc: 'Thermal derate profile under sun + wind.' },
    { id: 'EF1', group: 'E', sector: 'EVAL', depends_on: ['BL1'], estimate_ms: 27248, tps_min: 71, tps_max: 120, work_desc: 'Self‑righting trials on padded slope.' },
    { id: 'EG1', group: 'E', sector: 'EVAL', depends_on: ['BK1', 'BT1'], estimate_ms: 28317, tps_min: 80, tps_max: 97, work_desc: 'Black‑box log integrity; fault injection.' },
    { id: 'EH1', group: 'E', sector: 'EVAL', depends_on: ['BG2', 'BO1'], estimate_ms: 12889, tps_min: 45, tps_max: 85, work_desc: 'Grasp success rate vs randomized basket loads.' },
    { id: 'EI1', group: 'E', sector: 'EVAL', depends_on: ['BH2'], estimate_ms: 24619, tps_min: 74, tps_max: 113, work_desc: 'Ice reflectance compensation field validation.' },
    { id: 'EJ1', group: 'E', sector: 'EVAL', depends_on: ['BR1'], estimate_ms: 17879, tps_min: 54, tps_max: 95, work_desc: 'High wind fan‑tunnel test; gust rejection.' },
    { id: 'EK1', group: 'E', sector: 'EVAL', depends_on: ['PE1', 'BJ1'], estimate_ms: 21180, tps_min: 69, tps_max: 83, work_desc: 'Operator usability dry‑run; SOP rehearsal.' },
    { id: 'EL1', group: 'E', sector: 'EVAL', depends_on: ['BS1', 'EA3'], estimate_ms: 12727, tps_min: 60, tps_max: 113, work_desc: 'Energy per meter vs spec; regression tracking.' },
    { id: 'EM1', group: 'E', sector: 'EVAL', depends_on: ['EA1', 'EG1'], estimate_ms: 25208, tps_min: 65, tps_max: 105, work_desc: 'Multi‑hour continuous run; MTBF snapshot.' },
    { id: 'EN1', group: 'E', sector: 'EVAL', depends_on: ['BI1', 'BG1', 'BT1'], estimate_ms: 20326, tps_min: 73, tps_max: 119, work_desc: 'Recovery drill: comms blackout → safe kneel → reboot.' },
    { id: 'EO1', group: 'E', sector: 'EVAL', depends_on: ['EA2', 'EB1'], estimate_ms: 18244, tps_min: 64, tps_max: 105, work_desc: 'Sensor calibration drift check after descent.' },
    { id: 'EP1', group: 'E', sector: 'EVAL', depends_on: ['BW1'], estimate_ms: 24650, tps_min: 58, tps_max: 103, work_desc: 'Ingress protection validation with mist+dust.' },
    { id: 'EQ1', group: 'E', sector: 'EVAL', depends_on: ['BN1', 'BB2'], estimate_ms: 25037, tps_min: 57, tps_max: 86, work_desc: 'Cold‑start test at dawn.' },
    { id: 'ER1', group: 'E', sector: 'EVAL', depends_on: ['BJ1', 'BF1'], estimate_ms: 18742, tps_min: 59, tps_max: 91, work_desc: 'Trail courtesy test: pause for hikers, resume smoothly.' },
    { id: 'ES1', group: 'E', sector: 'EVAL', depends_on: ['BZ1'], estimate_ms: 12294, tps_min: 44, tps_max: 119, work_desc: 'Rope anchor static load test.' },
    { id: 'ET1', group: 'E', sector: 'EVAL', depends_on: ['BY1', 'EA3'], estimate_ms: 16205, tps_min: 61, tps_max: 84, work_desc: 'Footstep planner performance vs incline dataset.' },
    { id: 'EU1', group: 'E', sector: 'EVAL', depends_on: ['BD1', 'EA3'], estimate_ms: 21341, tps_min: 61, tps_max: 115, work_desc: 'Snow patch crossing with microspikes.' },
    { id: 'EV1', group: 'E', sector: 'EVAL', depends_on: ['BF1', 'EA3'], estimate_ms: 10545, tps_min: 70, tps_max: 104, work_desc: 'Descending brake tests; toe/heel first policy.' },
    { id: 'EW1', group: 'E', sector: 'EVAL', depends_on: ['BC1', 'EA2'], estimate_ms: 27771, tps_min: 78, tps_max: 98, work_desc: 'IMU bias & LiDAR occlusion scenarios.' },
    { id: 'EX1', group: 'E', sector: 'EVAL', depends_on: ['PF2', 'BA5'], estimate_ms: 23606, tps_min: 65, tps_max: 104, work_desc: 'Media capture dry‑run; camera mounts stable for launch day?' },

    // -----------------------------
    // Deploy
    // -----------------------------
    { id: 'DA1', group: 'D', sector: 'DEPLOY', depends_on: ['EA1'], estimate_ms: 19657, tps_min: 47, tps_max: 107, work_desc: 'Prepare production build: fasteners, harnesses, and protective shells.' },
    { id: 'DA2', group: 'D', sector: 'DEPLOY', depends_on: ['EA2'], estimate_ms: 11452, tps_min: 53, tps_max: 100, work_desc: 'Stage charging dock and on-site tools at mountain base.' },
    { id: 'DB1', group: 'D', sector: 'DEPLOY', depends_on: ['EB1'], estimate_ms: 17742, tps_min: 42, tps_max: 93, work_desc: 'Finalize safety checklist and operator SOP for haul cycles.' },
    { id: 'DA3', group: 'D', sector: 'DEPLOY', depends_on: ['EA3'], estimate_ms: 29450, tps_min: 55, tps_max: 104, work_desc: 'Package, label, and ship final unit to the trailhead.' },

    { id: 'DB2', group: 'D', sector: 'DEPLOY', depends_on: ['PF1', 'DA1'], estimate_ms: 29398, tps_min: 51, tps_max: 82, work_desc: 'Assemble field kit (spares, tools, adhesives).' },
    { id: 'DC1', group: 'D', sector: 'DEPLOY', depends_on: ['DA3'], estimate_ms: 17307, tps_min: 77, tps_max: 112, work_desc: 'Vehicle load‑out and weight balance.' },
    { id: 'DC2', group: 'D', sector: 'DEPLOY', depends_on: ['PD1', 'DA2'], estimate_ms: 27006, tps_min: 64, tps_max: 113, work_desc: 'Scout team brief: routes, weather, comms.' },
    { id: 'DD1', group: 'D', sector: 'DEPLOY', depends_on: ['BB4', 'DA2'], estimate_ms: 12324, tps_min: 51, tps_max: 112, work_desc: 'Basecamp power: generator + solar mats.' },
    { id: 'DD2', group: 'D', sector: 'DEPLOY', depends_on: ['BI1', 'DA2'], estimate_ms: 23252, tps_min: 53, tps_max: 91, work_desc: 'Camp mesh network setup & test.' },
    { id: 'DE1', group: 'D', sector: 'DEPLOY', depends_on: ['EC2', 'EK1'], estimate_ms: 24098, tps_min: 44, tps_max: 97, work_desc: 'Safety officer sign‑off.' },
    { id: 'DF1', group: 'D', sector: 'DEPLOY', depends_on: ['PA5', 'DA3'], estimate_ms: 25026, tps_min: 71, tps_max: 93, work_desc: 'Permit check‑in with rangers.' },
    { id: 'DG1', group: 'D', sector: 'DEPLOY', depends_on: ['DB1', 'EK1'], estimate_ms: 23626, tps_min: 64, tps_max: 109, work_desc: 'Pre‑ascent checklist dry‑run with timer.' },
    { id: 'DH1', group: 'D', sector: 'DEPLOY', depends_on: ['EX1', 'DC2'], estimate_ms: 29506, tps_min: 64, tps_max: 115, work_desc: 'Public launch livestream (optional, for the story).' },
    { id: 'DI1', group: 'D', sector: 'DEPLOY', depends_on: ['EN1', 'DA2'], estimate_ms: 23040, tps_min: 62, tps_max: 112, work_desc: 'Stage rescue kit and med kit at switchback 3.' },
    { id: 'DJ1', group: 'D', sector: 'DEPLOY', depends_on: ['DA3'], estimate_ms: 19765, tps_min: 77, tps_max: 114, work_desc: 'Deploy trail signage: “Robots at work.”' },
    { id: 'DK1', group: 'D', sector: 'DEPLOY', depends_on: ['EA3', 'DA2'], estimate_ms: 27577, tps_min: 74, tps_max: 111, work_desc: 'Base loop dry‑run at mountain; 2× laps.' },
    { id: 'DL1', group: 'D', sector: 'DEPLOY', depends_on: ['DE1', 'DG1', 'DK1'], estimate_ms: 29741, tps_min: 68, tps_max: 107, work_desc: 'Go/No‑Go review.' },
    { id: 'DM1', group: 'D', sector: 'DEPLOY', depends_on: ['DL1', 'DC2'], estimate_ms: 18702, tps_min: 64, tps_max: 104, work_desc: 'Summit attempt: select start window.' },
    { id: 'DN1', group: 'D', sector: 'DEPLOY', depends_on: ['DM1'], estimate_ms: 13920, tps_min: 80, tps_max: 82, work_desc: 'Post‑run teardown & base debrief.' },
    { id: 'DO1', group: 'D', sector: 'DEPLOY', depends_on: ['DN1'], estimate_ms: 21259, tps_min: 72, tps_max: 120, work_desc: 'Data offload + log triage.' },
    { id: 'DP1', group: 'D', sector: 'DEPLOY', depends_on: ['DO1'], estimate_ms: 23544, tps_min: 73, tps_max: 114, work_desc: 'Postmortem write‑up & highlight reel.' },
    { id: 'DQ1', group: 'D', sector: 'DEPLOY', depends_on: ['DC2'], estimate_ms: 20227, tps_min: 72, tps_max: 81, work_desc: 'Procure extra snacks—operator morale matters.' },
    { id: 'DR1', group: 'D', sector: 'DEPLOY', depends_on: ['DN1'], estimate_ms: 23331, tps_min: 57, tps_max: 83, work_desc: 'Battery recycling & disposal compliance.' },
    { id: 'DS1', group: 'D', sector: 'DEPLOY', depends_on: ['DN1'], estimate_ms: 21312, tps_min: 42, tps_max: 94, work_desc: 'Thank‑you notes to rangers and volunteers.' },
  ],
};

export default humanoidPlan;
