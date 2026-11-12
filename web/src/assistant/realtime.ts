import type { RealtimeEvent } from "./types";
export interface Connection {
  open(
    microphone: boolean,
    exchange: (sdp: string) => Promise<string>,
  ): Promise<void>;
  send(event: Record<string, unknown>): void;
  microphone(enabled: boolean): Promise<void>;
  close(): void;
}
export type ConnectionHandlers = {
  event: (event: RealtimeEvent) => void;
  failure: (message: string) => void;
};
export class WebRTCConnection implements Connection {
  private pc: RTCPeerConnection | null = null;
  private channel: RTCDataChannel | null = null;
  private sender: RTCRtpSender | null = null;
  private stream: MediaStream | null = null;
  private handlers: ConnectionHandlers;
  private closed = false;
  private microphoneGeneration = 0;
  private openReject: ((error: Error) => void) | null = null;
  constructor(handlers: ConnectionHandlers) {
    this.handlers = handlers;
  }
  async open(microphone: boolean, exchange: (sdp: string) => Promise<string>) {
    if (typeof RTCPeerConnection === "undefined")
      throw new Error(
        "Этот браузер не поддерживает WebRTC. Используйте актуальный Chrome, Firefox или Safari.",
      );
    const pc = new RTCPeerConnection();
    this.pc = pc;
    // Outgoing microphone only. No Audio element or receive transceiver exists.
    this.sender = pc.addTransceiver("audio", { direction: "sendonly" }).sender;
    pc.ontrack = (event) => {
      event.track.stop();
    };
    pc.onconnectionstatechange = () => {
      if (this.closed) return;
      if (["failed", "disconnected", "closed"].includes(pc.connectionState))
        this.handlers.failure("Аудиосоединение прервано. Подключитесь снова.");
    };
    const channel = pc.createDataChannel("oai-events");
    this.channel = channel;
    channel.onmessage = (event) => {
      if (
        this.closed ||
        typeof event.data !== "string" ||
        event.data.length > 1 << 20
      )
        return;
      try {
        const parsed: RealtimeEvent = JSON.parse(event.data);
        if (typeof parsed.type === "string") this.handlers.event(parsed);
      } catch {
        this.handlers.failure("Получено некорректное событие Realtime.");
      }
    };
    channel.onclose = () => {
      if (!this.closed)
        this.handlers.failure("Канал ассистента закрыт. Подключитесь снова.");
    };
    if (microphone) await this.microphone(true);
    if (this.closed) throw new Error("Подключение отменено.");
    await pc.setLocalDescription(await pc.createOffer());
    const answer = await exchange(pc.localDescription?.sdp || "");
    if (this.closed) throw new Error("Подключение отменено.");
    await pc.setRemoteDescription({ type: "answer", sdp: answer });
    if (channel.readyState !== "open")
      await new Promise<void>((resolve, reject) => {
        this.openReject = reject;
        const timeout = setTimeout(() => {
          this.openReject = null;
          reject(
            new Error("Не удалось открыть канал Realtime. Проверьте сеть."),
          );
        }, 20000);
        channel.onopen = () => {
          clearTimeout(timeout);
          this.openReject = null;
          resolve();
        };
        const originalReject = this.openReject;
        this.openReject = (error) => {
          clearTimeout(timeout);
          originalReject(error);
        };
      });
  }
  send(event: Record<string, unknown>) {
    if (this.closed || this.channel?.readyState !== "open")
      throw new Error("Ассистент не подключён.");
    this.channel.send(JSON.stringify(event));
  }
  async microphone(enabled: boolean) {
    const generation = ++this.microphoneGeneration;
    if (!enabled) {
      const stream = this.stream;
      this.stream = null;
      stream?.getTracks().forEach((t) => t.stop());
      await this.sender?.replaceTrack(null);
      return;
    }
    if (this.stream) return;
    if (!navigator.mediaDevices?.getUserMedia)
      throw new Error(
        "Микрофон доступен только на localhost или HTTPS и в поддерживаемом браузере.",
      );
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });
    } catch (e) {
      if (e instanceof DOMException && e.name === "NotAllowedError")
        throw new Error(
          "Доступ к микрофону запрещён. Разрешите его в настройках браузера.",
        );
      if (e instanceof DOMException && e.name === "NotFoundError")
        throw new Error(
          "Микрофон не найден. Можно отправить текстовую команду.",
        );
      throw new Error("Не удалось включить микрофон.");
    }
    if (this.closed || generation !== this.microphoneGeneration) {
      stream.getTracks().forEach((t) => t.stop());
      return;
    }
    try {
      await this.sender?.replaceTrack(stream.getAudioTracks()[0]);
      if (this.closed || generation !== this.microphoneGeneration) {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }
      this.stream = stream;
    } catch {
      stream.getTracks().forEach((t) => t.stop());
      throw new Error("Не удалось подключить аудиотрек.");
    }
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.microphoneGeneration++;
    this.openReject?.(new Error("Подключение отменено."));
    this.openReject = null;
    this.stream?.getTracks().forEach((t) => t.stop());
    this.stream = null;
    if (this.channel) {
      this.channel.onclose = null;
      this.channel.onmessage = null;
      this.channel.onopen = null;
      this.channel.close();
      this.channel = null;
    }
    if (this.pc) {
      this.pc.ontrack = null;
      this.pc.onconnectionstatechange = null;
      this.pc.close();
      this.pc = null;
    }
    this.sender = null;
  }
}
