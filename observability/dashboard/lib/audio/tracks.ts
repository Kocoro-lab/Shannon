export type AudioSource =
  | { type: 'local'; src: string }
  | { type: 'youtube'; url: string };

export type Track = {
  id: string;
  title: string;
  source: AudioSource;
};

// Place your audio files under `public/audio/` and update the mapping below.
// Example local files are placeholders; replace with your own.
export const tracks: Track[] = [
  {
    id: 'work-music-for-serious-grind-stay-aligned',
    title: 'Work Music for Serious Grind | Stay Aligned',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=MYW0TgV67RE&list=RDMYW0TgV67RE&start_radio=1' },
  },
  {
    id: 'lofi-hip-hop-radio',
    title: 'lofi hip hop radio 📚 beats to relax/study to',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=jfKfPfyJRdk' },
  },
  {
    id: 'flight-facilities-clair-de-lune-feat-christine-hoberg',
    title: 'Flight Facilities - Clair De Lune feat. Christine Hoberg',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=Jcu1AHaTchM&list=RDJcu1AHaTchM&start_radio=1' },
  },
  {
    id: 'work-music-for-progress-trust-the-process',
    title: 'Work Music for Progress | Trust the Process',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=Efkc-AMB96c' },
  },
  {
    id: 'work-music-for-ambition-build-the-future',
    title: 'Work Music for Ambition | Build the Future',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=UgT24uQx7-I' },
  },
  {
    id: 'work-music-for-deep-hustle-quiet-but-relentless',
    title: 'Work Music for Deep Hustle | Quiet but Relentless',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=5O-Nmzhkz_4&t=11s' },
  },
  {
    id: 'work-music-for-momentum-let-the-flow-build',
    title: 'Work Music for Momentum | Let the Flow Build',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=rH0iZluJ2fY' },
  },
  {
    id: 'work-music-for-clear-focus-calm-and-clear',
    title: 'Work Music for Clear Focus | Calm and Clear',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=fAxCoB2VUlc' },
  },
  {
    id: 'work-music-for-deep-work-focus-with-progress',
    title: 'Work Music for Deep Work | Focus With Progress',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=WZt8hjTwP20' },
  },
  {
    id: 'flight-facilities-live-at-airfields-sydney-full-concert',
    title: 'Flight Facilities - Live At Airfields, Sydney (Full Concert)',
    source: { type: 'youtube', url: 'https://youtu.be/ts-6KyJUDWY?si=Ib_PS5sd9fwLpzTI' },
  },
];

export const radio: Track[] = [
  {
    id: 'sfo',
    title: 'SFO ATC',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=mOec9Fu3Jz0' },
  },
  {
    id: 'las-vegas',
    title: 'Las Vegas ATC',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=Z_iF0OHUuz8' },
  },
  {
    id: 'jfk',
    title: 'JFK ATC',
    source: { type: 'youtube', url: 'https://www.youtube.com/watch?v=xq_kuLD8T0A' },
  }
]