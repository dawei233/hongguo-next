import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

/// 应用内「日记」日志服务 — 实时记录播放与系统事件，方便在电视与手机端快速排查问题。
class DiaryService {
  static final List<String> _entries = <String>[];
  static const int maxEntries = 500;
  static final ValueNotifier<int> revision = ValueNotifier<int>(0);

  /// 记录一条日记
  static void add(String message) {
    final now = DateTime.now();
    final timeStr =
        '${now.hour.toString().padLeft(2, '0')}:${now.minute.toString().padLeft(2, '0')}:${now.second.toString().padLeft(2, '0')}.${(now.millisecond ~/ 100)}';
    final entry = '[$timeStr] $message';
    _entries.add(entry);
    if (_entries.length > maxEntries) {
      _entries.removeRange(0, _entries.length - maxEntries);
    }
    revision.value++;
    debugPrint('[Diary] $message');
    unawaited(_persist(entry));
  }

  /// [红果漫剧补丁] 日记落盘：Windows/桌面端便于用户直接把日志文件发给我们排查。
  /// 路径：<应用支持目录>/diary.log（Windows = %APPDATA%\duanju\duanju_app\）
  /// 文件上限 2MB，超出后截断重写，避免无限增长。
  static const int _maxLogBytes = 2 * 1024 * 1024;
  static File? _logFile;
  static Future<void>? _logInit;
  static bool _logFailed = false;

  static Future<File?> _resolveLogFile() async {
    if (_logFile != null) return _logFile;
    if (_logFailed) return null;
    _logInit ??= () async {
      try {
        final dir = await getApplicationSupportDirectory();
        await dir.create(recursive: true);
        _logFile = File('${dir.path}${Platform.pathSeparator}diary.log');
      } catch (_) {
        _logFailed = true;
      }
    }();
    await _logInit;
    return _logFile;
  }

  static Future<void> _persist(String entry) async {
    try {
      final file = await _resolveLogFile();
      if (file == null) return;
      if (file.existsSync() && file.lengthSync() > _maxLogBytes) {
        await file.writeAsString('--- 日志已截断 ---\n');
      }
      await file.writeAsString('$entry\n', mode: FileMode.append, flush: false);
    } catch (_) {
      _logFailed = true;
    }
  }

  /// 日记文件路径（供界面展示，便于用户定位并发送）
  static Future<String> logFilePath() async {
    final file = await _resolveLogFile();
    return file?.path ?? '（无法写入日志文件）';
  }

  /// 获取当前所有日记副本
  static List<String> get entries => List<String>.unmodifiable(_entries);

  /// 清空日记
  static void clear() {
    _entries.clear();
    revision.value++;
  }

  /// 获取合并后的完整日志文本
  static String get fullText => _entries.join('\n');

  /// 复制全部日记到剪贴板
  static Future<void> copyToClipboard(BuildContext context) async {
    final text = fullText;
    await Clipboard.setData(ClipboardData(text: text));
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        const SnackBar(
          content: Text('已复制播放日记到剪贴板'),
          duration: Duration(seconds: 2),
        ),
      );
    }
  }

  /// 在界面上弹出交互式日记弹窗（兼容 TV 遥控器与手机触屏）
  static Future<void> showDiaryDialog(BuildContext context) {
    return showDialog<void>(
      context: context,
      builder: (dialogContext) {
        return AlertDialog(
          backgroundColor: const Color(0xFF1E1E24),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(16),
            side: BorderSide(color: Colors.white.withValues(alpha: 0.1)),
          ),
          title: Row(
            children: [
              const Icon(Icons.receipt_long_rounded, color: Colors.amberAccent),
              const SizedBox(width: 8),
              const Expanded(
                child: Text(
                  '播放调试日记',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 18,
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ),
              IconButton(
                icon: const Icon(Icons.copy_rounded, color: Colors.white70),
                tooltip: '复制全部日记',
                onPressed: () => copyToClipboard(dialogContext),
              ),
              IconButton(
                icon: const Icon(Icons.delete_outline_rounded, color: Colors.white70),
                tooltip: '清空日记',
                onPressed: () => clear(),
              ),
            ],
          ),
          content: SizedBox(
            width: double.maxFinite,
            height: MediaQuery.of(dialogContext).size.height * 0.65,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // [红果漫剧补丁] 显示日志文件路径：Windows 用户可直接去该目录取
                // diary.log 发给开发者（内存日记上限 500 条，文件是完整记录）。
                FutureBuilder<String>(
                  future: logFilePath(),
                  builder: (context, snapshot) {
                    final pathText = snapshot.data;
                    if (pathText == null) return const SizedBox.shrink();
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: SelectableText(
                        '日志文件：$pathText',
                        style: const TextStyle(color: Colors.white54, fontSize: 11),
                      ),
                    );
                  },
                ),
                Expanded(child: _buildEntryList(dialogContext)),
              ],
            ),
          ),
        );
      },
    );
  }

  /// 日记条目列表（从弹窗内容中抽出，便于在外层叠加「日志文件路径」）
  static Widget _buildEntryList(BuildContext dialogContext) {
    final scrollController = ScrollController();
    return AnimatedBuilder(
              animation: revision,
              builder: (context, _) {
                final list = entries;
                if (list.isEmpty) {
                  return const Center(
                    child: Text(
                      '暂无日记记录',
                      style: TextStyle(color: Colors.white38),
                    ),
                  );
                }
                return Container(
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Colors.black.withValues(alpha: 0.4),
                    borderRadius: BorderRadius.circular(8),
                    border: Border.all(
                      color: Colors.white.withValues(alpha: 0.08),
                    ),
                  ),
                  child: Scrollbar(
                    controller: scrollController,
                    thumbVisibility: true,
                    child: ListView.builder(
                      controller: scrollController,
                      itemCount: list.length,
                      itemBuilder: (context, index) {
                        final item = list[list.length - 1 - index]; // 倒序显示最新
                        Color textColor = Colors.white70;
                        final lower = item.toLowerCase();
                        if (lower.contains('err') ||
                            lower.contains('fail') ||
                            lower.contains('异常') ||
                            lower.contains('失败') ||
                            lower.contains('exception')) {
                          textColor = const Color(0xFFFF6B6B);
                        } else if (lower.contains('成功') ||
                            lower.contains('ok') ||
                            lower.contains('success')) {
                          textColor = const Color(0xFF51CF66);
                        } else if (lower.contains('open') ||
                            lower.contains('initialize') ||
                            lower.contains('play') ||
                            lower.contains('url')) {
                          textColor = const Color(0xFF74C0FC);
                        } else if (lower.contains('warn') ||
                            lower.contains('403') ||
                            lower.contains('404')) {
                          textColor = const Color(0xFFFCC419);
                        }
                        return Padding(
                          padding: const EdgeInsets.symmetric(vertical: 2.0),
                          child: SelectableText(
                            item,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 12,
                              color: textColor,
                            ),
                          ),
                        );
                      },
                    ),
                  ),
                );
              },
    );
  }
}
