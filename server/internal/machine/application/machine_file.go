package application

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mayfly-go/internal/machine/domain/entity"
	"mayfly-go/internal/machine/domain/repository"
	"mayfly-go/internal/machine/infrastructure/machine"
	"mayfly-go/pkg/biz"
	"mayfly-go/pkg/logx"
	"mayfly-go/pkg/model"
	"os"
	"strings"

	"github.com/pkg/sftp"
)

type MachineFile interface {
	// 分页获取机器文件信息列表
	GetPageList(condition *entity.MachineFile, pageParam *model.PageParam, toEntity any, orderBy ...string) *model.PageResult[any]

	// 根据条件获取
	GetMachineFile(condition *entity.MachineFile, cols ...string) error

	// 根据id获取
	GetById(id uint64, cols ...string) *entity.MachineFile

	Save(entity *entity.MachineFile)

	Delete(id uint64)

	// 获取文件关联的机器信息，主要用于记录日志使用
	// GetMachine(fileId uint64) *machine.Info

	// 检查文件路径，并返回机器id
	GetMachineCli(fileId uint64, path ...string) *machine.Cli

	/**  sftp 相关操作 **/

	// 创建目录
	MkDir(fid uint64, path string) (*machine.Info, error)

	// 创建文件
	CreateFile(fid uint64, path string) (*machine.Info, error)

	// 读取目录
	ReadDir(fid uint64, path string) []fs.FileInfo

	// 获取指定目录内容大小
	GetDirSize(fid uint64, path string) string

	// 获取文件stat
	FileStat(fid uint64, path string) string

	// 读取文件内容
	ReadFile(fileId uint64, path string) (*sftp.File, *machine.Info, error)

	// 写文件
	WriteFileContent(fileId uint64, path string, content []byte) (*machine.Info, error)

	// 文件上传
	UploadFile(fileId uint64, path, filename string, reader io.Reader) (*machine.Info, error)

	// 移除文件
	RemoveFile(fileId uint64, path ...string) (*machine.Info, error)

	Copy(fileId uint64, toPath string, paths ...string) (*machine.Info, error)

	Mv(fileId uint64, toPath string, paths ...string) (*machine.Info, error)

	Rename(fileId uint64, oldname string, newname string) (*machine.Info, error)
}

func newMachineFileApp(machineFileRepo repository.MachineFile, machineRepo repository.Machine) MachineFile {
	return &machineFileAppImpl{machineRepo: machineRepo, machineFileRepo: machineFileRepo}

}

type machineFileAppImpl struct {
	machineFileRepo repository.MachineFile
	machineRepo     repository.Machine
}

// 分页获取机器脚本信息列表
func (m *machineFileAppImpl) GetPageList(condition *entity.MachineFile, pageParam *model.PageParam, toEntity any, orderBy ...string) *model.PageResult[any] {
	return m.machineFileRepo.GetPageList(condition, pageParam, toEntity, orderBy...)
}

// 根据条件获取
func (m *machineFileAppImpl) GetMachineFile(condition *entity.MachineFile, cols ...string) error {
	return m.machineFileRepo.GetMachineFile(condition, cols...)
}

// 根据id获取
func (m *machineFileAppImpl) GetById(id uint64, cols ...string) *entity.MachineFile {
	return m.machineFileRepo.GetById(id, cols...)
}

// 保存机器文件配置
func (m *machineFileAppImpl) Save(entity *entity.MachineFile) {
	biz.NotNil(m.machineRepo.GetById(entity.MachineId, "Name"), "该机器不存在")

	if entity.Id != 0 {
		m.machineFileRepo.UpdateById(entity)
	} else {
		m.machineFileRepo.Create(entity)
	}
}

// 根据id删除
func (m *machineFileAppImpl) Delete(id uint64) {
	m.machineFileRepo.Delete(id)
}

func (m *machineFileAppImpl) ReadDir(fid uint64, path string) []fs.FileInfo {
	mcli := m.GetMachineCli(fid, path)
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}

	fis, err := mcli.GetSftpCli().ReadDir(path)
	biz.ErrIsNilAppendErr(err, "读取目录失败: %s")
	return fis
}

func (m *machineFileAppImpl) GetDirSize(fid uint64, path string) string {
	mcli := m.GetMachineCli(fid, path)
	res, err := mcli.Run(fmt.Sprintf("du -sh %s", path))
	if err != nil {
		// 若存在目录为空，则可能会返回如下内容。最后一行即为真正目录内容所占磁盘空间大小
		//du: cannot access ‘/proc/19087/fd/3’: No such file or directory\n
		//du: cannot access ‘/proc/19087/fdinfo/3’: No such file or directory\n
		//18G     /\n
		if res == "" {
			panic(biz.NewBizErr(fmt.Sprintf("获取目录大小失败: %s", err.Error())))
		}
		strs := strings.Split(res, "\n")
		res = strs[len(strs)-2]

		if !strings.Contains(res, "\t") {
			panic(biz.NewBizErr(res))
		}
	}
	// 返回 32K\t/tmp\n
	return strings.Split(res, "\t")[0]
}

func (m *machineFileAppImpl) FileStat(fid uint64, path string) string {
	mcli := m.GetMachineCli(fid, path)
	res, err := mcli.Run(fmt.Sprintf("stat -L %s", path))
	biz.ErrIsNil(err, res)
	return res
}

func (m *machineFileAppImpl) MkDir(fid uint64, path string) (*machine.Info, error) {
	mcli := m.GetMachineCli(fid, path)
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}

	err := mcli.GetSftpCli().MkdirAll(path)
	return mcli.GetMachine(), err
}

func (m *machineFileAppImpl) CreateFile(fid uint64, path string) (*machine.Info, error) {
	mcli := m.GetMachineCli(fid, path)
	file, err := mcli.GetSftpCli().Create(path)
	biz.ErrIsNilAppendErr(err, "创建文件失败: %s")
	defer file.Close()
	return mcli.GetMachine(), err
}

func (m *machineFileAppImpl) ReadFile(fileId uint64, path string) (*sftp.File, *machine.Info, error) {
	mcli := m.GetMachineCli(fileId, path)
	// 读取文件内容
	fc, err := mcli.GetSftpCli().Open(path)
	return fc, mcli.GetMachine(), err
}

// 写文件内容
func (m *machineFileAppImpl) WriteFileContent(fileId uint64, path string, content []byte) (*machine.Info, error) {
	mcli := m.GetMachineCli(fileId, path)
	mi := mcli.GetMachine()
	f, err := mcli.GetSftpCli().OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE|os.O_RDWR)
	if err != nil {
		return mi, err
	}
	defer f.Close()
	f.Write(content)
	return mi, err
}

// 上传文件
func (m *machineFileAppImpl) UploadFile(fileId uint64, path, filename string, reader io.Reader) (*machine.Info, error) {
	mcli := m.GetMachineCli(fileId, path)
	mi := mcli.GetMachine()
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}

	createfile, err := mcli.GetSftpCli().Create(path + filename)
	if err != nil {
		return mi, err
	}
	defer createfile.Close()
	io.Copy(createfile, reader)
	return mi, err
}

// 删除文件
func (m *machineFileAppImpl) RemoveFile(fileId uint64, path ...string) (*machine.Info, error) {
	mcli := m.GetMachineCli(fileId, path...)
	minfo := mcli.GetMachine()

	// 优先使用命令删除（速度快），sftp需要递归遍历删除子文件等
	res, err := mcli.Run(fmt.Sprintf("rm -rf %s", strings.Join(path, " ")))
	if err == nil {
		return minfo, nil
	}
	logx.Errorf("使用命令rm删除文件失败: %s", res)

	sftpCli := mcli.GetSftpCli()
	for _, p := range path {
		err = sftpCli.RemoveAll(p)
		if err != nil {
			break
		}
	}
	return minfo, err
}

func (m *machineFileAppImpl) Copy(fileId uint64, toPath string, paths ...string) (*machine.Info, error) {
	mcli := m.GetMachineCli(fileId, paths...)
	mi := mcli.GetMachine()
	res, err := mcli.Run(fmt.Sprintf("cp -r %s %s", strings.Join(paths, " "), toPath))
	if err != nil {
		return mi, errors.New(res)
	}
	return mi, err
}

func (m *machineFileAppImpl) Mv(fileId uint64, toPath string, paths ...string) (*machine.Info, error) {
	mcli := m.GetMachineCli(fileId, paths...)
	mi := mcli.GetMachine()
	res, err := mcli.Run(fmt.Sprintf("mv %s %s", strings.Join(paths, " "), toPath))
	if err != nil {
		return mi, errors.New(res)
	}
	return mi, err
}

func (m *machineFileAppImpl) Rename(fileId uint64, oldname string, newname string) (*machine.Info, error) {
	mcli := m.GetMachineCli(fileId, newname)
	return mcli.GetMachine(), mcli.GetSftpCli().Rename(oldname, newname)
}

// 获取文件机器cli
func (m *machineFileAppImpl) GetMachineCli(fid uint64, inputPath ...string) *machine.Cli {
	biz.IsTrue(fid != 0, "文件id不能为空")
	mf := m.GetById(fid)
	biz.NotNil(mf, "文件不存在")
	for _, path := range inputPath {
		// 接口传入的地址需为配置路径的子路径
		biz.IsTrue(strings.HasPrefix(path, mf.Path), "无权访问该目录或文件: %s", path)
	}
	return GetMachineApp().GetCli(mf.MachineId)
}
