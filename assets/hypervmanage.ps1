Function IfNull($a, $b) { if ($null -eq $a) { $b } else { $a } }

Function getresult {
    Return [PSCustomObject]@{
        Success      = $false;
        ErrorMessage = "";
        PayLoad      = $null;
    }
}

Function getkuttivmobject {
    param(
        [string] $machineName
    )

    $vm = Hyper-V\Get-VM -Name $machineName -ErrorAction Stop | 
    Select-Object Name, 
    @{Name = "IPAddress"; Expression = { IfNull $_.NetworkAdapters[0].IPAddresses[0] "" } }, 
    @{Name = "State"; Expression = { $_.State.ToString() } } 
    
    $vmresult = [PSCustomObject]@{
        Machine = $vm
    }

    Return $vmresult
}

Function Test-Driver {
    $result = getresult
    $testresult = [PSCustomObject]@{HypervisorPresent = $false; Permissions = $false; PermissionLevel = "" }

    $testresult.HypervisorPresent = @(Get-CimInstance Win32_ComputerSystem).HypervisorPresent

    $isadmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole] "Administrator")
    If ($isadmin) {
        $testresult.Permissions = $true
        $testresult.PermissionLevel = "administrator"
    }
    Else {
        $ishypervadmin = @([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(([System.Security.Principal.SecurityIdentifier]::new("S-1-5-32-578")))
        If ($ishypervadmin) {
            $testresult.Permissions = $true
            $testresult.PermissionLevel = "hypervadministrator"
        }
    }

    $result.Success = $testresult.HypervisorPresent -and $testresult.Permissions
    If (-not $testresult.HypervisorPresent) {
        $result.ErrorMessage = "Hyper-V not enabled"
    }
    ElseIf (-not $testresult.Permissions) {
        $result.ErrorMessage = "user should be an administrator, or a member of the Hyper-V Administrators group"
    }
    $result.PayLoad = $testresult

    $result | ConvertTo-Json 
}

Function Get-KuttiVMList() {
    $result = getresult
    Try {
        $vmlist = Hyper-V\Get-VM | Select-Object Name,
                    @{Name = "IPAddress"; Expression = { IfNull $_.NetworkAdapters[0].IPAddresses[0] "" } },
                    @{Name = "State"; Expression = { $_.State.ToString() } } 
        $vmresult = [PSCustomObject] @{VMList = $vmlist }
        $result.Success = $true
        $result.PayLoad = $vmresult
    }
    Catch {
        $result.ErrorMessage = "could not retrieve VMs"
    }

    $result | ConvertTo-Json
}

Function Get-KuttiVM() {
    param (
        [string]
        $machineName
    )

    $result = getresult
    If ([string]::IsNullOrEmpty($machineName)) {
        $result.ErrorMessage = "machine name not specified"
    }
    Else {
        Try {
            $vmresult = getkuttivmobject $machineName
    
            $result.Success = $true
            $result.PayLoad = $vmresult
        }
        Catch {
            $result.ErrorMessage = $_.ToString()
        }
    }

    $result | ConvertTo-Json
}

Function Start-KuttiVM() {
    param (
        [string]
        $machineName
    )

    $result = getresult
    If ([string]::IsNullOrEmpty($machineName)) {
        $result.ErrorMessage = "machine name not specified"
    }
    Else {
        Try {
            Hyper-V\Start-VM -Name $machineName -ErrorAction Stop -WarningAction Stop
            $result.Success = $true
        }
        Catch {
            $result.ErrorMessage = $_.ToString()
        }
    }

    $result | ConvertTo-Json
}

Function Stop-KuttiVM() {
    param (
        [string]
        $machineName,
        [bool]
        $force
    )

    $forceparam = @{}

    If ($force) {
        $forceparam = @{TurnOff = $true; Confirm = $false }
    }
    Else {
        $forceparam = @{Force = $true; Confirm = $false }
    }

    $result = getresult
    If ([string]::IsNullOrEmpty($machineName)) {
        $result.ErrorMessage = "machine name not specified"
    }
    Else {
        Try {
            Hyper-V\Stop-VM -Name $machineName -ErrorAction Stop -WarningAction Stop @forceparam
            $result.Success = $true
        }
        Catch {
            $result.ErrorMessage = $_.ToString()
        }
    }

    $result | ConvertTo-Json
}

Function New-KuttiVM() {
    param (
        [string]
        $machineName,
        [string]
        $machinePath,
        [string]
        $vhdpath
    )

    $result = getresult
    If ([string]::IsNullOrEmpty($machineName) -or [string]::IsNullOrEmpty($machinepath) -or [string]::IsNullOrEmpty($vhdpath)) {
        $result.ErrorMessage = "machine name or machinepath or vhdpath not specified"
    }
    Else {
        Try {
            $newvm = Hyper-V\New-VM -Name $machineName -Generation 1 -Path $machinePath -VHDPath $vhdpath -SwitchName "Default Switch"
            Hyper-V\Set-VM $newvm -StaticMemory -MemoryStartupBytes 2147483648 -ProcessorCount 2 -CheckpointType Disabled

            $result.Success = $true
        }
        Catch {
            $result.ErrorMessage = $_.ToString()
        }
    }

    $result | ConvertTo-Json
}

Function Wait-KuttiVM() {
    param (
        [string]
        $machineName,
        [string]
        $machineStatus,
        [int64]
        $timeOutSeconds
    )

    $result = getresult
    If ([string]::IsNullOrEmpty($machineName) -or [string]::IsNullOrEmpty($machineStatus)) {
        $result.ErrorMessage = "machine name or machinestatus not specified"
    }
    Else {
        $params = @{}
        Switch ($machineStatus) {
            { $_ -in "Starting", "Started" } { $params = @{For = "IPAddress" } }
            { $_ -in "Stopping", "Stopped" } { $params = @{For = "Reboot" } }
        }

        If ($timeOutSeconds -eq 0) {
            $timeOutSeconds = 25
        }

        Try {
            Hyper-V\Wait-VM -ErrorAction Stop Hyper-V\-VMName $machineName -Timeout $timeOutSeconds @params

            $vmresult = getkuttivmobject $machineName
            $result.Success = $true
            $result.PayLoad = $vmresult
        }
        Catch {
            $result.ErrorMessage = $_.ToString()
        }
    }

    $result | ConvertTo-Json
}

Function Remove-KuttiVM() {
    param (
        [string]
        $machineName
    )

    $result = getresult
    If ([string]::IsNullOrEmpty($machineName)) {
        $result.ErrorMessage = "machine name not specified"
    }
    Else {
        Try {
            Hyper-V\Remove-VM -Name $machineName -ErrorAction Stop -Force

            $result.Success = $true
        }
        Catch {
            $result.ErrorMessage = $_.ToString()
        }
    }

    $result | ConvertTo-Json
}

If ($args.Count -eq 0) {
    $result = getresult
    $result.ErrorMessage = "interface arguments not specified"
    
    $result | ConvertTo-Json
    break
}

Switch ($args[0].ToString().ToLowerInvariant()) {
    "checkdriver" { Test-Driver }
    "listmachines" { Get-KuttiVMList }
    "getmachine" { Get-KuttiVM $args[1] }
    "startmachine" { Start-KuttiVM $args[1] }
    "stopmachine" { Stop-KuttiVM $args[1] $false }
    "forcestopmachine" { Stop-KuttiVM $args[1] $true }
    "waitmachine" { Wait-KuttiVM $args[1] $args[2] $args[3] }
    "deletemachine" { Remove-KuttiVM $args[1] }
    "newmachine" { New-KuttiVM $args[1] $args[2] $args[3] }
    Default {
        $result = getresult
        $result.ErrorMessage = "invalid interface argument: " + $args[0]
        
        $result | ConvertTo-Json    
    }
}
