import { restingFrame } from './lib/avatar/kit/frame';
import { FRAME_SIZE } from './lib/avatar/kit/math';

const size = 512;
const canvas = document.getElementById('c') as HTMLCanvasElement;
canvas.width = size;
canvas.height = size;
const context = canvas.getContext('2d')!;
context.fillStyle = '#ffffff';
context.fillRect(0, 0, size, size);
context.save();
context.translate(size / 2, size / 2);
context.scale(size / FRAME_SIZE, size / FRAME_SIZE);

const { body, eyes } = restingFrame(100);
context.fillStyle = '#000000';
context.beginPath();
context.moveTo(body[0].x, body[0].y);
for (let i = 1; i < body.length; i++) context.lineTo(body[i].x, body[i].y);
context.closePath();
context.fill();

context.fillStyle = '#ffffff';
for (const eye of eyes) {
  context.beginPath();
  context.moveTo(eye[0].x, eye[0].y);
  for (let i = 1; i < eye.length; i++) context.lineTo(eye[i].x, eye[i].y);
  context.closePath();
  context.fill();
}
context.restore();
