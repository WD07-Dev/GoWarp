# 테스트 배치

- `extensions/`: VM·데스크톱·통합 테스트가 함께 사용하는 Go 확장 샘플
- `extensions/discord/`: 별도 Go 모듈로 분리한 Rich Presence 구현·작업자·블록 통합 테스트
- `cli/`: 헤드리스 실행 도구와 프로젝트 통합 테스트
- `fixtures/projects/`: 작은 자체 SB3 테스트 프로젝트
- `fixtures/media/`: 직접 생성한 오디오 테스트 자료

비공개 구현과 동시성을 검사하는 단위 테스트는 검사 대상 코드 옆에 둡니다.
공용 확장 샘플과 공개 API를 통한 통합 테스트는 이 디렉터리에 모읍니다.
테스트는 `ref` 디렉터리가 없어도 실행됩니다.
Discord 테스트는 실제 계정에 연결하거나 상태를 게시하지 않습니다.

일반 테스트와 Discord 확장 테스트를 각각 실행합니다.

```sh
go test ./app/... ./core/... ./platform/... ./test/...
go -C test/extensions/discord test ./...
```

Discord의 외부 의존성은 해당 테스트 모듈의 `go.mod`에서 관리합니다.
본체의 `go.mod`와 런타임 SDK에는 포함하지 않습니다.
루트 `./build.sh`는 배포본을 빌드하며 테스트는 위 명령으로 실행합니다.
GUI 관련 테스트에는 디스플레이가 필요하므로 Linux 헤드리스 환경에서는 Xvfb를 사용합니다.
별도의 테스트 생성·복사·overlay 과정은 없습니다.

## Rich Presence 확장

완성본은 `extensions/examples/cubesterRichPresence.js`입니다. TurboWarp에서 사용자 정의
확장 파일로 불러온 뒤 SB3를 저장해 GoWarp에서 패키징합니다. 기존
`cubesterRichPresence` ID와 블록 이름을 유지합니다.

사용 순서: 앱 ID로 연결 → details/state 및 큰·작은 이미지 설정 → 시간 표시 설정 →
`update rich presence`. 이미지에는 Discord 앱의 이미지 에셋 키 또는 공개 이미지 URL을
넣습니다. 로컬 파일 경로는 사용할 수 없습니다. 이미지 설명은 마우스를 올렸을 때 표시됩니다.
JSON의 타임스탬프는 Unix 초 단위입니다. 오류는 `rich presence last error`로 확인합니다.
일반 브라우저에서는 데이터를 편집할 수 있지만 Discord 연결에는 GoWarp 또는 RPC를 제공하는
호스트가 필요합니다. Discord 데스크톱 앱이 실행 중이어야 실제 상태가 표시됩니다.

테스트는 완성본 JS의 주석 속 Go 코드가 `extensions/discord/module.go`와
일치하는지 검사하고 이 구현을 실행합니다.
테스트는 실제 Discord 연결을 사용하지 않습니다.
이미지 URL 지원은 [Discord 공식 문서](https://discord.com/developers/docs/social-sdk/classdiscordpp_1_1ActivityAssets.html)를 참고하세요.
