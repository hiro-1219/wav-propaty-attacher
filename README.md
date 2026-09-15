# WAV Property Attacher

WAVアルバムをMusicBrainz / AcoustIDで識別し、rekordbox 7向けの
ID3v2.3タグ付きAIFFコピーを生成するGo/Wailsデスクトップアプリです。
元のWAVファイルは変更しません。

検索時はWAVの既存RIFF情報、フォルダ名、ファイル名からアルバム名、
アーティスト、年、曲名を推定します。MusicBrainzの厳密検索と緩い検索を
段階的に行い、候補が曖昧な場合だけAcoustID音響指紋を3曲から最大7曲まで
追加して候補を統合します。

GUIはスクロールするページ遷移を使わない1画面構成です。アルバム指定、
候補選択、曲割当、ジャケット確認、AIFF出力を3つのパネルで確認できます。
候補一覧と曲一覧が画面に収まらない場合だけ、各リスト内でスクロールします。

## プライバシー

音源ファイル自体を外部サービスへアップロードしません。

- Chromaprintの`fpcalc`はローカルで動作します。
- 音響指紋検索を明示的に有効にした場合だけ、指紋と再生秒数を
  AcoustIDへ送ります。指紋から音声を再生・復元することはできませんが、
  楽曲を特定する情報ではあります。
- MusicBrainzへはRecording IDまたは検索文字列を送ります。
- Cover Art Archiveからジャケットを取得します。
- テレメトリーや指紋登録・音源投稿機能はありません。

## 対応入力

- RIFF/WAVEまたはPCMのWAVE_FORMAT_EXTENSIBLE
- 非圧縮PCM 16 / 24 bit
- 44.1 / 48 / 88.2 / 96 / 192 kHz
- mono / stereo

Float WAV、32 bit PCM、圧縮WAV、RF64/BW64、3チャンネル以上は、
rekordbox互換出力の対象外として安全に拒否します。

## 開発ビルド

```powershell
go mod download
go test ./...
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
wails build -trimpath -webview2 error
```

Windows版は`build/bin/wav-property-attacher.exe`へ生成されます。Windowsでは
WebView2 Runtimeが必要ですが、Windows 11には通常インストールされています。
本環境では`wails doctor`によりWebView2、Go、Node.js、npmを確認済みです。
詳細は[Wailsのインストール手順](https://wails.io/docs/gettingstarted/installation/)を
参照してください。

### fpcalc

音響指紋検索にはChromaprintの`fpcalc`が必要です。CIの配布成果物には、
SHA-256を検証した公式Chromaprint 1.6.1を実行ファイル隣の
`tools/fpcalc.exe`（Windows）または`tools/fpcalc`（macOS/Linux）として
同梱します。開発時は同じ場所へ置くか、PATHへ追加してください。

https://github.com/acoustid/chromaprint/releases

文字検索のみの場合は`fpcalc`もAcoustIDキーも不要です。

### AcoustIDキー

AcoustIDのアプリケーションキーを取得し、画面のキー欄へ入力します。
これはユーザー個人の投稿キーではなく、アプリケーション登録キーです。

https://acoustid.org/new-application

## 操作

1. アルバムフォルダまたはその中のWAVを左側の領域へドロップします。
   「フォルダを選択」から指定することもできます。ドロップ時は自動読込します。
2. 必要ならAcoustIDキーを入力し、説明を確認して指紋送信を有効にします。
3. 「アルバムを検索」を押し、Release候補と曲割当を確認します。曲ごとの
   セレクトボックスで、別トラックへの割当、出力除外、未割当を指定できます。
   自動抽出されたアルバム名、アーティスト、年は検索前に修正できます。
   見つからない場合は詳細欄へMusicBrainzのRelease URLまたはMBIDを指定できます。
4. ジャケットのプレビューを確認します。必要なら右側の画像領域へJPEG/PNGを
   ドロップするか、クリックしてローカル画像を選択します。
5. 「rekordbox用AIFFを生成」を押します。
6. 作成された`rekordbox`フォルダをrekordbox 7のCollectionへドラッグします。

既に取り込んだAIFFを再生成した場合は、rekordboxで対象曲を選択し、
`Track > Reload Tag`を実行してください。

## 出力と安全性

出力は元アルバム内の`rekordbox/`へ作成し、Disc階層とベースファイル名を
維持します。AIFFは一時領域で全曲を生成・検証してから配置されます。
`.wav-property-attacher.json`には、元WAV・出力AIFFの音声ハッシュ、
MusicBrainz ID、曲対応を記録します。

同名AIFFが存在し、本アプリが同じ元WAVから生成したと確認できない場合は
上書きしません。

## 現在の範囲

初期バージョンでは、ファイル名変更、BPM/BeatGrid/CUE生成、rekordboxの
データベース直接編集、DJ機器へのUSB書き出し、WAV以外の入力には対応しません。
