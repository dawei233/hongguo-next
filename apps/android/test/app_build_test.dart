import 'dart:convert';

import 'package:hongguo_next/app_build.dart';
import 'package:hongguo_next/core_bridge.dart';
import 'package:hongguo_next/local_profiles.dart';
import 'package:hongguo_next/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const red = FixtureRepository.free;
  const other = FixtureRepository.vip;

  test('restricted sources stay hidden until the source gate unlocks', () async {
    SharedPreferences.setMockInitialValues({'source': 'huangdou'});
    final store = testStore(await SharedPreferences.getInstance());
    // 新语义（2026-10-16）：不再编译期分版（上游「红果鉴/真果鉴」两版废弃），
    // 应用身份统一为 hongguo-next。
    expect(appSlug, 'hongguo-next');
    // hongguo-next 默认 6 个 primary 源（红果/韩小圈/鬼片/青空 + 新增西饭/河马），
    // 其余 7 个 restricted 源默认隐藏，由密码锁解锁后可见。
    expect(store.sources.length, 6);
    // values = knownValues（不再编译期分版）：dsd 在注册表里且可用，
    // 是否允许由密码锁与用户权限决定，而不是编译期裁剪。
    expect(SourceSite.values.any((source) => source.id == 'dsd'), isTrue);
    expect(SourceSite.isAvailable('dsd'), isTrue);
    expect(SourceSite.isKnown('dsd'), isTrue);
    expect(SourceSite.byId('dsd').name, '帝果');
    // dsd 是 restricted 源：密码锁默认激活（_gateOff=false 且未解锁），所以默认不允许。
    expect(store.allowsSource('dsd'), isFalse);
    expect(store.source, 'hongguo');
    await store.enableSourceGate('666666');
    expect(store.sourcesUnlocked, isTrue);
    // 解锁后 primary 6 + restricted 7 = 13 个站源全可见（加了西饭/河马）。
    expect(store.sources.length, 13);
    expect(store.allowsSource('dsd'), isTrue);
    store.dispose();
  });

  test(
    'visibility filtering preserves favorites and history through backup restore',
    () async {
      final history = [
        for (final drama in [red, other])
          WatchEntry(
            drama: drama,
            episode: 1,
            position: 12,
            duration: 60,
            updatedAt: DateTime(2026, 9, 19),
          ).toJson(),
      ];
      SharedPreferences.setMockInitialValues({
        'source': 'huangdou',
        'favorites': jsonEncode([red.toJson(), other.toJson()]),
        'history': jsonEncode(history),
      });
      final store = testStore(await SharedPreferences.getInstance());
      // 管理员默认可见 6 个 primary 源；显式启用密码锁，验证解锁后行为一致。
      await store.enableSourceGate('666666');
      // 解锁后 restricted 源(huangdou)也可见，红果与黄豆的记录都在。
      expect(store.favorites.length, 2);
      expect(store.history.length, 2);
      expect(store.isFavorite(other.id), isTrue);
      expect(store.watched(other.id) != null, isTrue);
      await store.toggleFavorite(red);
      final backup = await store.exportBackup();
      final library =
          (jsonDecode(backup)['libraries'] as Map)['default'] as Map;
      // 导出读的是原始磁盘记录（exportBackup 不做可见性过滤），
      // 受限站源 other(huangdou) 依旧保留在导出里。
      expect(
        (library['favorites'] as List).map((row) => (row as Map)['id']),
        contains(other.id),
      );
      expect(library['history'], hasLength(2));
      await store.importBackup(backup);
      expect(store.preferences.getString('source'), 'huangdou');
      // 新语义：导入用 replace 覆盖全部偏好，密码锁凭据一并清掉，
      // _gateOff=false → restricted 源(huangdou)默认隐藏：
      // 历史只剩 primary 源 red，favorites（只剩 other/huangdou）被门控隐藏。
      expect(store.history, hasLength(1));
      expect(store.favorites.map((drama) => drama.id), isEmpty);
      // 关闭密码锁（管理员）后全部站源可见，证明受限源记录确实被备份保留。
      await store.disableSourceGate();
      expect(store.history, hasLength(2));
      expect(store.favorites.map((drama) => drama.id), [other.id]);
      store.dispose();
    },
  );

  test(
    'a restored foreign-source profile keeps its identity and permissions',
    () async {
      SharedPreferences.setMockInitialValues({
        ...await gatePreferences(),
        'profiles': jsonEncode([
          LocalProfile(
            id: 'default',
            name: '管理员',
            admin: true,
            salt: '0' * 32,
            pinHash: '1' * 64,
          ).toJson(),
          const LocalProfile(
            id: 'viewer',
            name: '已有用户',
            sources: ['huangdou'],
            download: false,
          ).toJson(),
        ]),
        'activeProfile': 'viewer',
        'profile.viewer.source': 'huangdou',
      });
      final store = testStore(await SharedPreferences.getInstance());
      await unlockGate(store);
      expect(store.profile.id, 'viewer');
      expect(store.profile.admin, isFalse);
      expect(store.profile.sources, ['huangdou']);
      expect(store.canDownload, isFalse);
      // 密码锁解锁后 restricted 源(huangdou)可见且可选。
      expect(store.source, 'huangdou');
      // 看红果不允许是用户权限（viewer 只授权 huangdou），与编译期分版无关。
      expect(store.allowsSource('hongguo'), isFalse);
      expect(store.allowsSource('huangdou'), isTrue);
      store.dispose();
    },
  );

  test('restored DSD profile data shows after the gate unlocks', () async {
    SharedPreferences.setMockInitialValues({
      ...await gatePreferences(),
      'profiles': jsonEncode([
        LocalProfile(
          id: 'default',
          name: '管理员',
          admin: true,
          salt: '0' * 32,
          pinHash: '1' * 64,
        ).toJson(),
        const LocalProfile(
          id: 'viewer',
          name: '帝果旧用户',
          sources: ['dsd'],
          download: false,
        ).toJson(),
      ]),
      'activeProfile': 'viewer',
      'profile.viewer.source': 'dsd',
    });
    final store = testStore(await SharedPreferences.getInstance());
    await unlockGate(store);
    expect(store.configurationError, isNull);
    expect(store.profile.sources, ['dsd']);
    // 解锁后全部 13 个站源可见，viewer 只授权 dsd，所以列表里只有它。
    expect(store.sources.map((source) => source.id), ['dsd']);
    expect(store.source, 'dsd');
    expect(store.allowsSource('dsd'), isTrue);
    store.dispose();
  });

  test(
    'background requests reject unavailable sources before native I/O',
    () async {
      final repository = NativeRepository(background: true);
      final denied = [
        ...SourceSite.allValues
            .where((source) => !SourceSite.isAvailable(source.id))
            .map((source) => source.id),
        'unknown',
      ];
      for (final source in denied) {
        final drama = Drama(id: '$source:123', source: source, title: '合成数据');
        final episode = Episode({'id': '1'}, 1);
        for (final request in [
          () => repository.catalog(source),
          () => repository.cached(source),
          () => repository.sourceStatus(source),
          () => repository.startSourceJob(source, 'update'),
          () => repository.cancelSourceJob(source),
          () => repository.detail(drama),
          () => repository.cover(drama),
          () => repository.resolve(drama, episode),
          () => repository.resolveOnline(drama, episode),
          () => repository.enqueueDownloads(DramaDetail(drama, [episode]), [
            episode,
          ]),
          () => repository.localPlayback(drama, episode),
        ]) {
          await expectLater(
            request(),
            throwsA(
              isA<AppFailure>().having(
                (error) => error.message,
                'message',
                '当前版本不包含此站源',
              ),
            ),
          );
        }
      }
    },
  );
}
