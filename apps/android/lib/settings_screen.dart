import 'dart:convert';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'core_bridge.dart';
import 'app_theme.dart';
import 'background_downloads.dart';
import 'local_store.dart';
import 'profiles_screen.dart';
import 'app_build.dart';
import 'sources_screen.dart';
import 'ui/components/hn_settings_group.dart';
import 'ui/layout.dart';
import 'ui/tokens.dart';
import 'widgets.dart';
import 'resource_settings_screen.dart';
import 'lan_screen.dart';
import 'diary_service.dart';

String storageSize(int bytes) {
  if (bytes < 0) return '暂不可用';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) {
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
}

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  bool _busy = false;
  String? _message;

  /// §4.3.2 平板横屏两栏下「当前分组」索引；手机单列布局不用。
  int _pane = 0;

  Future<void> _chooseTheme() async {
    final selected = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('外观主题'),
        children: [
          RadioGroup<String>(
            groupValue: widget.store.themeMode,
            onChanged: (value) => Navigator.pop(context, value),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final mode in ['light', 'dark', 'system'])
                  RadioListTile<String>(
                    value: mode,
                    title: Text(AppTheme.label(mode)),
                    subtitle: mode == 'system'
                        ? const Text('随设备的深色模式自动切换')
                        : null,
                  ),
              ],
            ),
          ),
        ],
      ),
    );
    if (selected != null && mounted) {
      await saveUserChange(context, () => widget.store.setThemeMode(selected));
    }
  }

  Future<void> _backup(bool restore) async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (!restore) {
        final content = await widget.store.exportBackup();
        final saved = await FilePicker.saveFile(
          fileName:
              '$appSlug-backup-${DateTime.now().toIso8601String().substring(0, 10)}.json',
          bytes: Uint8List.fromList(utf8.encode(content)),
          mimeType: 'application/json',
        );
        if (saved != null && mounted) setState(() => _message = '备份已保存');
      } else {
        final file = await FilePicker.pickFile(
          type: FileType.custom,
          allowedExtensions: ['json'],
        );
        if (file == null || !mounted) return;
        final size = await file.length();
        if (size == null || size > 8 * 1024 * 1024) {
          throw const FormatException('备份文件过大或无法读取');
        }
        final content = utf8.decode(await file.readAsBytes());
        final data = widget.store.validateBackup(content);
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('恢复备份？'),
            content: Text(
              '包含 ${(data['profiles'] as List).length} 个用户。将替换本机的用户、追剧、观看记录和偏好设置；已下载视频保留。恢复后使用备份内的管理员密码登录。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('恢复'),
              ),
            ],
          ),
        );
        if (accepted != true || !mounted) return;
        await widget.store.importBackup(content);
        if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// 设置页的**5 个场景分组**（§4.3.1 归属表 + §4.3.2 线框图为准）。
  ///
  /// ⚠ 规范 §0/§3.2 那句写的是「4 个场景分组」，但 §4.3.1 归属表与 §4.3.2
  /// 线框图都列出 **5 个**（通用 / 播放与下载 / 内容与站源 / 数据与同步 / 关于）
  /// —— §0 那句漏了「通用」。以**明细章节**为准。已记入实施报告。
  ///
  /// **单份数据供两种布局**：手机竖屏渲染成分组卡（§4.3.3），平板横屏渲染成
  /// 「左分组导航 240 + 右内容」（§4.3.2）。与 `_railDestinations` 同一思路
  /// —— 定义一份、渲染多处，避免两处各写一遍而分叉。
  List<(String, List<Widget>)> get _settingGroups => [
      (
        '通用',
        <Widget>[
          ListTile(
            key: const ValueKey('theme-setting'),
            leading: const Icon(Icons.palette_outlined),
            title: const Text('外观主题'),
            subtitle: Text(AppTheme.label(widget.store.themeMode)),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: _chooseTheme,
          ),
        ],
      ),
      (
        '播放与下载',
        <Widget>[
          if (widget.store.canDownload)
            ListTile(
              leading: const Icon(Icons.download_outlined),
              title: const Text('下载偏好'),
              subtitle: Text(widget.store.downloadPreferences.qualityLabel),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) =>
                      DownloadPreferencesScreen(store: widget.store),
                ),
              ),
            ),
        ],
      ),
      (
        '内容与站源',
        <Widget>[
          if (widget.repository.supportsSourceManagement)
            ListTile(
              leading: const Icon(Icons.dns_outlined),
              title: const Text('站源管理'),
              subtitle: const Text('独立更新、连接与播放检测'),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => SourcesScreen(
                    repository: widget.repository,
                    store: widget.store,
                  ),
                ),
              ),
            ),
        ],
      ),
      (
        '数据与同步',
        <Widget>[
          ListTile(
            key: const ValueKey('lan-settings'),
            leading: const Icon(Icons.devices_rounded),
            title: const Text('设备互联'),
            subtitle: const Text('局域网自动同步追剧与推送播放'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: () => openLanSync(context),
          ),
          ListTile(
            leading: const Icon(Icons.people_outline),
            title: const Text('用户管理'),
            subtitle: Text('当前：${widget.store.profile.name}'),
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => ProfilesScreen(store: widget.store),
              ),
            ),
          ),
          if (widget.store.canDownload)
            ListTile(
              leading: const Icon(Icons.folder_outlined),
              title: const Text('下载目录与空间'),
              subtitle: const Text('查看存储用量、迁移已下载文件'),
              onTap: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => StorageScreen(
                    repository: widget.repository,
                    store: widget.store,
                  ),
                ),
              ),
            ),
          if (widget.store.profile.admin) ...[
            SwitchListTile(
              value: widget.store.autoExport,
              title: const Text('下载完成后自动导出 Emby'),
              subtitle: const Text(
                '在下载目录的 exports 中生成视频和海报 URL 元数据，可将该目录加入 Emby 媒体库。',
              ),
              onChanged: _busy
                  ? null
                  : (value) async {
                      try {
                        if (value) {
                          await BackgroundDownloads.ensureStarted();
                        }
                        await widget.store.setAutoExport(value);
                      } catch (error) {
                        if (mounted) {
                          setState(() => _message = error.toString());
                        }
                      }
                    },
            ),
            SwitchListTile(
              value: widget.store.exportPosters,
              title: const Text('同时导出海报文件'),
              subtitle: const Text('默认只写海报 URL。源站海报需要解密或外部读取失败时可开启。'),
              onChanged: _busy
                  ? null
                  : (value) => saveUserChange(
                      context,
                      () => widget.store.setExportPosters(value),
                    ),
            ),
            ListTile(
              leading: const Icon(Icons.backup_outlined),
              title: const Text('导出配置备份'),
              subtitle: const Text('包含本地用户、追剧、历史和设置，不含视频文件'),
              onTap: _busy ? null : () => _backup(false),
            ),
            ListTile(
              leading: const Icon(Icons.restore),
              title: const Text('恢复配置备份'),
              onTap: _busy ? null : () => _backup(true),
            ),
          ],
        ],
      ),
      (
        '关于',
        <Widget>[
          if (widget.store.profile.admin) ...[
            ListTile(
              leading: const Icon(Icons.settings_ethernet_rounded),
              title: const Text('网络与资源'),
              subtitle: const Text('代理、目录请求间隔、下载并发与站源目录'),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => ResourceSettingsScreen(
                    repository: widget.repository,
                    store: widget.store,
                  ),
                ),
              ),
            ),
          ],
          ListTile(
            key: const ValueKey('diary-setting'),
            leading: const Icon(Icons.receipt_long_rounded),
            title: const Text('播放调试日记'),
            subtitle: const Text('查看并复制应用运行与播放器的详细事件日志'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: () => DiaryService.showDiaryDialog(context),
          ),
        ],
      ),
  ];

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: AppBar(title: const Text('设置与备份')),
      body: LayoutBuilder(
        builder: (context, constraints) {
          // §4.3.2：平板横屏用两栏；其余（手机竖屏/横屏）用 §4.3.3 单列分组卡。
          // 形态判定复用 HnBreak（§2.1），不再另写宽度阈值。
          final groups = _settingGroups;
          if (HnBreak.formOf(constraints.biggest) == HnForm.tabletLandscape) {
            return _twoPane(context, groups);
          }
          return _singleColumn(context, groups);
        },
      ),
    ),
  );

  /// §4.3.3 手机：单列 + 分组卡。
  Widget _singleColumn(
    BuildContext context,
    List<(String, List<Widget>)> groups,
  ) => Center(
    child: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 720),
      child: ListView(
        padding: const EdgeInsets.all(HnSpacing.step4),
        children: [
          for (final (title, children) in groups)
            HnSettingsGroup(title: title, children: children),
          ..._statusWidgets(),
        ],
      ),
    ),
  );

  /// §4.3.2 平板横屏：左分组导航 240 + 右内容（最大 640）。
  ///
  /// 横屏有冗余宽度，把分组导航常驻后，"我要改播放行为"从"滚到那一组"
  /// 变成一次点击 —— 这正是 §0 记录的原始诉求。
  Widget _twoPane(BuildContext context, List<(String, List<Widget>)> groups) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    // 越界时夹回：分组数量变化后索引可能失效。
    final index = _pane.clamp(0, groups.length - 1);
    final (title, children) = groups[index];
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 240,
          child: ListView(
            padding: const EdgeInsets.symmetric(vertical: HnSpacing.step3),
            children: [
              for (final (i, entry) in groups.indexed)
                // §4.3.2：高 48，选中 primary-subtle 底 + onPrimarySubtle 字。
                // `ListTile` 没有 height 参数（其高度由内容与 dense 档决定），
                // 故用 SizedBox 固定 48 —— 规范给的是确定高度。
                SizedBox(
                  height: HnSettingsGroup.rowHeightWide,
                  child: ListTile(
                    key: ValueKey('settings-group-$i'),
                    selected: i == index,
                    selectedTileColor: tokens?.primarySubtle,
                    selectedColor: tokens?.onPrimarySubtle,
                    title: Text(entry.$1),
                    onTap: () => setState(() => _pane = i),
                  ),
                ),
            ],
          ),
        ),
        VerticalDivider(width: 1, color: tokens?.borderSubtle),
        Expanded(
          child: Center(
            child: ConstrainedBox(
              // §4.3.2：内容最大宽 640（避免单选按钮拉太开）。
              constraints: const BoxConstraints(maxWidth: 640),
              child: ListView(
                padding: const EdgeInsets.all(HnSpacing.step5),
                children: [
                  HnSettingsGroup(
                    title: title,
                    variant: HnSettingsVariant.plain,
                    children: children,
                  ),
                  ..._statusWidgets(),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }

  /// 进度条与消息（两种布局共用）—— 与分组数据同为"定义一份、多处使用"。
  List<Widget> _statusWidgets() => [
    if (_busy) const LinearProgressIndicator(),
    if (_message != null)
      Padding(
        padding: const EdgeInsets.all(HnSpacing.step4),
        child: SelectableText(_message!),
      ),
  ];
}

class StorageScreen extends StatefulWidget {
  const StorageScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<StorageScreen> createState() => _StorageScreenState();
}

class _StorageScreenState extends State<StorageScreen> {
  Map<String, dynamic>? _info;
  String? _error;
  bool _busy = false;
  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final info = await widget.repository.storage();
      if (mounted) {
        setState(() {
          _info = info;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _move() async {
    String? parent;
    if (Platform.isAndroid || Platform.isIOS) {
      final support = await getApplicationSupportDirectory();
      final directories = <String, String>{support.path: '应用内部存储'};
      if (Platform.isAndroid) {
        final external = await getExternalStorageDirectories() ?? [];
        for (var i = 0; i < external.length; i++) {
          directories[external[i].path] = i == 0
              ? '设备共享存储（应用目录）'
              : 'SD 卡 ${i + 1}（应用目录）';
        }
      } else {
        final documents = await getApplicationDocumentsDirectory();
        directories[documents.path] = '文件 App 可见目录';
      }
      if (!mounted) return;
      parent = await showDialog<String>(
        context: context,
        builder: (context) => SimpleDialog(
          title: const Text('选择下载位置'),
          children: [
            for (final entry in directories.entries)
              SimpleDialogOption(
                onPressed: () => Navigator.pop(context, entry.key),
                child: Text(entry.value),
              ),
          ],
        ),
      );
    } else {
      parent = await FilePicker.getDirectoryPath(dialogTitle: '选择下载保存位置');
    }
    if (parent == null || !mounted) return;
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('迁移已下载内容？'),
        content: Text(
          '将视频、合并成品及导出内容迁移到：\n$parent\n\n下载会先暂停，复制成功后清理旧目录。请保证目标有足够空间，并在完成后继续下载。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('迁移'),
          ),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.repository.moveDownloads(parent);
      await _refresh();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: !_busy,
    child: Scaffold(
      appBar: AppBar(title: const Text('下载目录与空间')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (_info != null) ...[
                Text(
                  '已使用 ${storageSize((_info!['bytes'] as num?)?.toInt() ?? 0)}',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 8),
                Text(
                  '剩余 ${storageSize((_info!['free'] as num?)?.toInt() ?? -1)} · ${_info!['files'] ?? 0} 个文件',
                ),
                const SizedBox(height: 24),
                const Text('当前下载目录'),
                const SizedBox(height: 8),
                SelectableText(_info!['directory'] as String? ?? ''),
                TextButton.icon(
                  onPressed: () => Clipboard.setData(
                    ClipboardData(text: _info!['directory'] as String? ?? ''),
                  ),
                  icon: const Icon(Icons.copy),
                  label: const Text('复制路径'),
                ),
                const SizedBox(height: 20),
                const Text('在下载页删除不需要的分集，在本地媒体页删除合并成品或 Emby 导出，可释放空间。'),
              ],
              if (_busy || (_info == null && _error == null))
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 20),
                  child: LinearProgressIndicator(),
                ),
              if (_busy) const Text('正在迁移，请保持应用运行…'),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Text(_error!),
                ),
              if (widget.store.profile.admin)
                FilledButton.icon(
                  onPressed: _busy ? null : _move,
                  icon: const Icon(Icons.drive_file_move_outline),
                  label: const Text('更改并迁移目录'),
                ),
              TextButton(
                onPressed: _busy ? null : _refresh,
                child: const Text('刷新用量'),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
